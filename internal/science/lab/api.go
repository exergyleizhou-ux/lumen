package lab

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"sync/atomic"

	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/hostedauth"
	"lumen/internal/lumenstore"
	"lumen/internal/permission"
	"lumen/internal/quota"
	"lumen/internal/runstate"
	"lumen/internal/runtimeevidence"
	"lumen/internal/science/lab/compute"
	"lumen/internal/science/lab/jupyter"
	"lumen/internal/science/lab/langgraph"
	"lumen/internal/science/lab/onlyoffice"
	"lumen/internal/science/lab/project"
	"lumen/internal/science/lab/provenance"
	labruntime "lumen/internal/science/lab/runtime"
	"lumen/internal/science/lab/workspace"
	"lumen/internal/science/native/brief"
	"lumen/internal/science/paths"
	"lumen/internal/science/research"
	"lumen/internal/skill"
	"lumen/internal/usage"
)

// API hosts lab REST + SSE handlers.
type API struct {
	sciDir           string
	version          string
	listenPort       int
	projects         *project.Store
	fleet            *labruntime.FleetManager
	local            LocalConfig
	turns            *turnPool
	ctrls            *controllerPool
	approvals        *approvalHub
	runs             *runstate.Manager
	usage            usage.Store
	quota            quota.Store
	approvalStore    approvalstate.Store
	artifactStore    artifact.Store
	evidence         runtimeevidence.Service
	platformProvider *config.ProviderConfig
	activeRuns       *sync.Map // run_id -> labActiveRun
	auth             *hostedauth.Verifier
	tenants          *tenantRegistry
	// activeMode is read by approval hub during a turn.
	modeMu     *sync.Mutex
	activeMode permission.Mode
	ownerModes map[runstate.Owner]permission.Mode
	startedAt  time.Time
	// metrics
	turnsTotal   *atomic.Uint64
	turnsFailed  *atomic.Uint64
	approvalsTot *atomic.Uint64
}

type labActiveRun struct {
	Owner  runstate.Owner
	Cancel context.CancelFunc
}

func labOwner(r *http.Request) runstate.Owner {
	if id, ok := hostedauth.FromContext(r.Context()); ok {
		return runstate.Owner{UserID: id.UserID, WorkspaceID: id.WorkspaceID}
	}
	return runstate.LocalOwner
}

// NewAPI builds the lab API.
func NewAPI(sciDir, version string, fleet *labruntime.FleetManager, listenPort int, runs *runstate.Manager) *API {
	if listenPort == 0 {
		listenPort = DefaultPort
	}
	if runs == nil {
		runs = runstate.NewManager(runstate.NewSQLiteStore(lumenstore.Default()))
	}
	var usageStore usage.Store = usage.NewMemoryStore()
	var approvalStore approvalstate.Store = approvalstate.NewMemoryStore()
	var artifactStore artifact.Store = artifact.NewMemoryStore()
	store := project.NewStore(sciDir)
	api := &API{
		sciDir:        sciDir,
		version:       version,
		listenPort:    listenPort,
		projects:      store,
		fleet:         fleet,
		local:         loadLocalConfig(sciDir),
		turns:         newTurnPool(MaxConcurrentTurns),
		ctrls:         newControllerPool(sciDir, fleet, store, MaxControllers),
		runs:          runs,
		usage:         usageStore,
		approvalStore: approvalStore,
		artifactStore: artifactStore,
		activeRuns:    new(sync.Map),
		activeMode:    permission.ModeDefault,
		modeMu:        new(sync.Mutex),
		ownerModes:    make(map[runstate.Owner]permission.Mode),
		startedAt:     time.Now(),
		turnsTotal:    new(atomic.Uint64),
		turnsFailed:   new(atomic.Uint64),
		approvalsTot:  new(atomic.Uint64),
	}
	if ur, ok := api.usage.(runtimeevidence.UsageReader); ok {
		api.evidence = runtimeevidence.Service{Runs: runs, Approvals: api.approvalStore, Artifacts: api.artifactStore, Usage: ur}
	}
	api.approvals = newApprovalHub(func() permission.Mode {
		api.modeMu.Lock()
		defer api.modeMu.Unlock()
		return api.activeMode
	})
	api.approvals.ownerModeFunc = api.ownerMode
	api.approvals.store = api.approvalStore
	return api
}

// Register mounts routes on mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/lab/health", a.handleHealth)
	mux.HandleFunc("/api/lab/readyz", a.handleReadyz)
	register := func(pattern string, handler func(*API, http.ResponseWriter, *http.Request)) {
		if a.auth != nil {
			wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				owner := labOwner(r)
				tenant, err := a.tenants.acquire(owner)
				if err != nil {
					w.Header().Set("Retry-After", "2")
					writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("tenant capacity reached; next_action=retry with backoff"))
					return
				}
				defer a.tenants.release(owner)
				r = r.WithContext(context.WithValue(r.Context(), tenantContextKey{}, tenant))
				// Keep the mature Lab handlers unchanged while binding every mutable
				// resource they use to this request's authenticated tenant. The copy
				// is request-local; shared concurrency/run coordination stays shared.
				scoped := *a
				scoped.sciDir = tenant.Root
				scoped.projects = tenant.Projects
				scoped.ctrls = tenant.Controllers
				scoped.local = loadLocalConfig(tenant.Root)
				handler(&scoped, w, r)
			})
			mux.Handle(pattern, a.auth.RequireFor(labPermission)(wrapped))
			return
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { handler(a, w, r) })
	}
	register("/api/lab/projects", (*API).handleProjects)
	register("/api/lab/projects/", (*API).handleProjectSub)
	register("/api/lab/skills", (*API).handleSkills)
	register("/api/lab/skills/", (*API).handleSkillsSub)
	register("/api/lab/config", (*API).handleConfig)
	register("/api/lab/chat", (*API).handleChat)
	register("/api/lab/runs/", (*API).handleRuns)
	register("/api/lab/approve", (*API).handleApprove)
	register("/api/lab/brief", (*API).handleBrief)
	register("/api/lab/artifacts", (*API).handleArtifacts)
	register("/api/lab/files", (*API).handleFiles)
	register("/api/lab/files/", (*API).handleFiles)
	register("/api/lab/provenance", (*API).handleProvenance)
	register("/api/lab/compute/ssh-hosts", (*API).handleComputeSSHHosts)
	register("/api/lab/compute/ssh-hosts/", (*API).handleComputeSSHHosts)
	register("/api/lab/compute/hosts", (*API).handleComputeSSHHosts) // alias
	register("/api/lab/compute/hosts/", (*API).handleComputeSSHHosts)
	register("/api/lab/compute/jobs", (*API).handleComputeJobs)
	register("/api/lab/compute/jobs/", (*API).handleComputeJob)
	register("/api/lab/c2d/algorithms", (*API).handleC2DAlgorithms)
	register("/api/lab/bridge/open", (*API).handleBridgeOpen)
	register("/api/lab/notebooks", (*API).handleNotebooks)
	register("/api/lab/notebooks/", (*API).handleNotebooks)
	register("/api/lab/langgraph/run", (*API).handleLangGraphRun)
	register("/api/lab/langgraph/history", (*API).handleLangGraphHistory)
	register("/api/lab/onlyoffice/callback", (*API).handleOnlyOfficeCallback)
	register("/api/lab/onlyoffice/session", (*API).handleOnlyOfficeSession)
}

type tenantContextKey struct{}

func tenantFromRequest(r *http.Request) *tenantResources {
	t, _ := r.Context().Value(tenantContextKey{}).(*tenantResources)
	return t
}

func labPermission(r *http.Request) string {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/lab/runs/") {
		if strings.HasSuffix(path, "/cancel") {
			return "run:cancel"
		}
		if strings.Contains(path, "/artifacts/") && strings.HasSuffix(path, "/download") {
			return "artifact:read"
		}
		return "run:read"
	}
	if path == "/api/lab/approve" {
		return "approval:decide"
	}
	if strings.HasPrefix(path, "/api/lab/artifacts") || strings.HasPrefix(path, "/api/lab/files") || strings.HasPrefix(path, "/api/lab/provenance") {
		if r.Method == http.MethodGet {
			return "artifact:read"
		}
	}
	return "lab:run"
}

func (a *API) beginActiveRun(parent context.Context, owner runstate.Owner, runID string, timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	ctx = runstate.WithRunID(ctx, runID)
	a.activeRuns.Store(runID, labActiveRun{Owner: owner, Cancel: cancel})
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			a.activeRuns.Delete(runID)
			cancel()
		})
	}
	return ctx, cleanup
}

func (a *API) cancelActiveRun(owner runstate.Owner, runID string) bool {
	value, ok := a.activeRuns.Load(runID)
	if !ok {
		return false
	}
	active, ok := value.(labActiveRun)
	if !ok || active.Owner != owner {
		return false
	}
	a.activeRuns.Delete(runID)
	active.Cancel()
	return true
}

func (a *API) handleRuns(w http.ResponseWriter, r *http.Request) {
	owner := labOwner(r)
	rel := strings.TrimPrefix(r.URL.Path, "/api/lab/runs/")
	if rel == "" || strings.Contains(rel, "..") {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid run path"))
		return
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		run, err := a.runs.GetOwned(owner, parts[0])
		if errors.Is(err, runstate.ErrRunNotFound) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("run not found"))
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"run": run})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "events" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("after must be a non-negative integer"))
				return
			}
			after = value
		}
		events, err := a.runs.EventsOwned(owner, parts[0], after)
		if errors.Is(err, runstate.ErrRunNotFound) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("run not found"))
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "run_id": parts[0], "after": after})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "artifacts" {
		if r.Method != http.MethodGet {
			writeErr(w, 405, fmt.Errorf("method not allowed"))
			return
		}
		if _, err := a.runs.GetOwned(owner, parts[0]); err != nil {
			writeErr(w, 404, fmt.Errorf("run not found"))
			return
		}
		items, err := a.artifactStore.ListRun(owner, parts[0])
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		if items == nil {
			items = []artifact.Record{}
		}
		writeJSON(w, 200, map[string]any{"artifacts": items})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "approvals" {
		if r.Method != http.MethodGet {
			writeErr(w, 405, fmt.Errorf("method not allowed"))
			return
		}
		if _, err := a.runs.GetOwned(owner, parts[0]); err != nil {
			writeErr(w, 404, fmt.Errorf("run not found"))
			return
		}
		items, err := a.approvalStore.ListRun(owner, parts[0])
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		safe := make([]map[string]any, 0, len(items))
		for _, item := range items {
			safe = append(safe, safeLabApprovalReview(item))
		}
		writeJSON(w, 200, map[string]any{"approvals": safe})
		return
	}
	if len(parts) == 4 && parts[0] != "" && parts[1] == "artifacts" && parts[2] != "" && parts[3] == "download" {
		if r.Method != http.MethodGet {
			writeErr(w, 405, fmt.Errorf("method not allowed"))
			return
		}
		if _, err := a.runs.GetOwned(owner, parts[0]); err != nil {
			writeErr(w, 404, fmt.Errorf("run not found"))
			return
		}
		items, err := a.artifactStore.ListRun(owner, parts[0])
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		var found *artifact.Record
		for i := range items {
			if items[i].ID == parts[2] {
				found = &items[i]
				break
			}
		}
		if found == nil {
			writeErr(w, 404, fmt.Errorf("artifact not found"))
			return
		}
		body, err := a.artifactStore.Open(r.Context(), owner, *found)
		if err != nil {
			writeErr(w, 404, fmt.Errorf("artifact not found"))
			return
		}
		defer body.Close()
		name := artifact.SafeName(found.Name)
		if name == "" {
			name = "artifact"
		}
		w.Header().Set("Content-Type", found.MIME)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, body)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "workbench-snapshot" {
		if r.Method != http.MethodGet {
			writeErr(w, 405, fmt.Errorf("method not allowed"))
			return
		}
		run, err := a.runs.GetOwned(owner, parts[0])
		if err != nil {
			writeErr(w, 404, fmt.Errorf("run not found"))
			return
		}
		events, err := a.runs.EventsOwned(owner, run.ID, 0)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		approvals, err := a.approvalStore.ListRun(owner, run.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		artifacts, err := a.artifactStore.ListRun(owner, run.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		pending := 0
		for _, approval := range approvals {
			if approval.Decision == nil && time.Now().UTC().Before(approval.ExpiresAt) {
				pending++
			}
		}
		var lastSeq uint64
		if len(events) > 0 {
			lastSeq = events[len(events)-1].Seq
		}
		writeJSON(w, 200, map[string]any{"workspace_id": run.WorkspaceID, "run_id": run.ID, "last_seq": lastSeq, "status": run.Status, "terminal": run.Status.Terminal(), "pending_approvals": pending, "verification": labWorkbenchVerification(events, run), "artifact_count": len(artifacts)})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "evidence" {
		if r.Method != http.MethodGet {
			writeErr(w, 405, fmt.Errorf("method not allowed"))
			return
		}
		bundle, err := a.evidence.Build(r.Context(), owner, parts[0])
		if errors.Is(err, runstate.ErrRunNotFound) {
			writeErr(w, 404, fmt.Errorf("run not found"))
			return
		}
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+parts[0]+`-evidence.zip"`)
		w.Write(bundle)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
			return
		}
		if _, err := a.runs.GetOwned(owner, parts[0]); errors.Is(err, runstate.ErrRunNotFound) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("run not found"))
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !a.cancelActiveRun(owner, parts[0]) {
			writeErr(w, http.StatusConflict, fmt.Errorf("run is not active"))
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "run_id": parts[0]})
		return
	}
	writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid run path"))
}

func safeLabApprovalReview(item approvalstate.Approval) map[string]any {
	return map[string]any{"id": item.ID, "run_id": item.RunID, "step_id": item.StepID, "tool_call_id": item.ToolCallID, "risk_level": item.RiskLevel, "effects": item.Effects, "estimated_cost_micros": item.EstimatedCostMicros, "created_at": item.CreatedAt, "expires_at": item.ExpiresAt, "decision": item.Decision, "execution_state": item.ExecutionState}
}

func labWorkbenchVerification(events []event.Event, run runstate.Run) string {
	state := "idle"
	for _, ev := range events {
		switch ev.Kind {
		case event.VerifyStarted:
			state = "running"
		case event.VerifyResult:
			if strings.Contains(ev.Text, "skipped") {
				state = "not_run"
			} else if ev.Level == event.LevelInfo {
				state = "passed"
			} else {
				state = "failed"
			}
		}
	}
	if run.StopReason == "verification_failed" || run.StopReason == "verification_incomplete" {
		return "failed"
	}
	if state == "idle" && run.Status.Terminal() {
		return "not_run"
	}
	return state
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sciCfg, _ := scienceConfig(a.sciDir)
	masked, adapter := providerStatus(sciCfg)
	rep := research.Report{}
	if rrep, err := research.Scan(paths.DataDir(a.sciDir)); err == nil {
		rep = rrep
	}
	ctrlTotal, ctrlBusy := a.ctrls.stats()
	ketcherDir := resolveKetcherDir(a.sciDir)
	ketcherOK := ketcherDir != ""
	jupyterOK := jupyter.IsAvailable()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"port":         a.listenPort,
		"panel":        "lumen://science-lab",
		"version":      a.version,
		"science_mode": sciCfg.ScienceMode,
		"uptime_sec":   int(time.Since(a.startedAt).Seconds()),
		"research_pack": map[string]any{
			"healthy":       rep.Healthy(),
			"bio_clients":   rep.BioLibPackages,
			"domain_tools":  rep.TotalDomainTools,
			"skills":        len(rep.Skills),
			"domains":       len(rep.Domains),
			"seed_examples": rep.SeedExamples,
		},
		"fleet_available": a.fleet != nil,
		"provider": map[string]any{
			"set":     masked != "" && masked != "—",
			"adapter": adapter,
		},
		"capacity": map[string]any{
			"turns_active":     a.turns.active(),
			"turns_capacity":   a.turns.capacity(),
			"controllers":      ctrlTotal,
			"controllers_busy": ctrlBusy,
			"max_controllers":  MaxControllers,
			"turns_total":      a.turnsTotal.Load(),
			"turns_failed":     a.turnsFailed.Load(),
			"approvals_total":  a.approvalsTot.Load(),
		},
		"limits": map[string]any{
			"max_concurrent_turns": MaxConcurrentTurns,
			"approval_timeout_sec": int(ApprovalTimeout.Seconds()),
			"turn_timeout_sec":     int(DefaultTurnTimeout.Seconds()),
		},
		"ketcher": map[string]any{
			"same_origin": ketcherOK,
			"path":        "/ketcher/",
			"dir":         ketcherDir,
		},
		"jupyter": map[string]any{
			"available": jupyterOK,
		},
		"onlyoffice": onlyoffice.Health(strings.TrimSpace(os.Getenv("LUMEN_ONLYOFFICE_URL"))),
		"langgraph":  a.langGraphHealth(),
	})
}

// handleReadyz is a stricter probe for orchestrators (Caddy/k8s).
func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Ready when server process is up; fleet may still be connecting (non-blocking).
	ready := a.turns.active() < a.turns.capacity()
	status := http.StatusOK
	body := map[string]any{
		"ready":        ready,
		"turns_active": a.turns.active(),
		"turns_cap":    a.turns.capacity(),
	}
	if !ready {
		status = http.StatusServiceUnavailable
		body["reason"] = "at capacity"
	}
	writeJSON(w, status, body)
}

func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := a.projects.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body struct {
			Title    string `json:"title"`
			Template string `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Title) == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("title required"))
			return
		}
		p, err := a.projects.Create(body.Title, body.Template)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleProjectSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/lab/projects/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodDelete {
			if _, err := a.projects.Get(slug); err != nil {
				if os.IsNotExist(err) {
					writeErr(w, http.StatusNotFound, fmt.Errorf("project not found"))
					return
				}
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			if err := a.projects.Delete(slug); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			var body struct {
				Title string `json:"title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			p, err := a.projects.Rename(slug, body.Title)
			if err != nil {
				if os.IsNotExist(err) {
					writeErr(w, http.StatusNotFound, err)
					return
				}
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, p)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p, err := a.projects.Get(slug)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		sessions, _ := a.projects.ListSessions(slug)
		writeJSON(w, http.StatusOK, map[string]any{"project": p, "sessions": sessions})
		return
	}
	// /api/lab/projects/:slug/sessions
	if parts[1] == "sessions" {
		if len(parts) == 2 {
			switch r.Method {
			case http.MethodGet:
				// ?q= full-text search across turns
				if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
					hits, err := a.projects.SearchSessions(slug, q, 50)
					if err != nil {
						writeErr(w, http.StatusInternalServerError, err)
						return
					}
					if hits == nil {
						hits = []project.SessionSearchHit{}
					}
					writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "count": len(hits), "q": q})
					return
				}
				sessions, err := a.projects.ListSessions(slug)
				if err != nil {
					writeErr(w, http.StatusInternalServerError, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "count": len(sessions)})
				return
			case http.MethodPost:
				var body struct {
					Title string `json:"title"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				sess, err := a.projects.CreateSession(slug, body.Title)
				if err != nil {
					writeErr(w, http.StatusBadRequest, err)
					return
				}
				writeJSON(w, http.StatusOK, sess)
				return
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}
		// /api/lab/projects/:slug/sessions/import  POST {title?, turns}
		if len(parts) == 3 && parts[2] == "import" && r.Method == http.MethodPost {
			var body struct {
				Title string         `json:"title"`
				Turns []project.Turn `json:"turns"`
				// also accept a full exported session object
				Session *project.Session `json:"session"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			title := body.Title
			turns := body.Turns
			if body.Session != nil {
				if title == "" {
					title = body.Session.Title
				}
				if len(turns) == 0 {
					turns = body.Session.Turns
				}
			}
			sess, err := a.projects.ImportSession(slug, title, turns)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			// return full with turns
			full, err := a.projects.GetSession(slug, sess.ID)
			if err != nil {
				writeJSON(w, http.StatusOK, sess)
				return
			}
			writeJSON(w, http.StatusOK, full)
			return
		}
		// /api/lab/projects/:slug/sessions/export-all?format=md|json
		if len(parts) == 3 && parts[2] == "export-all" && r.Method == http.MethodGet {
			a.handleSessionsExportAll(w, r, slug)
			return
		}
		// /api/lab/projects/:slug/sessions/search?q= (alias)
		if len(parts) == 3 && parts[2] == "search" && r.Method == http.MethodGet {
			q := r.URL.Query().Get("q")
			hits, err := a.projects.SearchSessions(slug, q, 50)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			if hits == nil {
				hits = []project.SessionSearchHit{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "count": len(hits), "q": q})
			return
		}
		// /api/lab/projects/:slug/sessions/:id[/export|/fork]
		if len(parts) >= 3 {
			sid := parts[2]
			if len(parts) == 4 && parts[3] == "export" && r.Method == http.MethodGet {
				a.handleSessionExport(w, r, slug, sid)
				return
			}
			if len(parts) == 4 && parts[3] == "fork" && r.Method == http.MethodPost {
				var body struct {
					Title string `json:"title"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				sess, err := a.projects.ForkSession(slug, sid, body.Title)
				if err != nil {
					if os.IsNotExist(err) {
						writeErr(w, http.StatusNotFound, err)
						return
					}
					writeErr(w, http.StatusBadRequest, err)
					return
				}
				writeJSON(w, http.StatusOK, sess)
				return
			}
			if len(parts) == 3 {
				switch r.Method {
				case http.MethodGet:
					sess, err := a.projects.GetSession(slug, sid)
					if err != nil {
						writeErr(w, http.StatusNotFound, err)
						return
					}
					writeJSON(w, http.StatusOK, sess)
					return
				case http.MethodPatch, http.MethodPut:
					var body struct {
						Title string `json:"title"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						writeErr(w, http.StatusBadRequest, err)
						return
					}
					sess, err := a.projects.RenameSession(slug, sid, body.Title)
					if err != nil {
						if os.IsNotExist(err) {
							writeErr(w, http.StatusNotFound, err)
							return
						}
						writeErr(w, http.StatusBadRequest, err)
						return
					}
					writeJSON(w, http.StatusOK, sess)
					return
				case http.MethodDelete:
					if err := a.projects.DeleteSession(slug, sid); err != nil {
						if os.IsNotExist(err) {
							writeErr(w, http.StatusNotFound, err)
							return
						}
						writeErr(w, http.StatusInternalServerError, err)
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": sid})
					return
				default:
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
			}
		}
	}
	http.NotFound(w, r)
}

// handleSessionsExportAll exports every session under a project as one md/json download.
func (a *API) handleSessionsExportAll(w http.ResponseWriter, r *http.Request, slug string) {
	list, err := a.projects.ListSessions(slug)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "md"
	}
	type fullSess struct {
		project.Session
	}
	var full []project.Session
	for _, s := range list {
		got, err := a.projects.GetSession(slug, s.ID)
		if err != nil {
			continue
		}
		full = append(full, got)
	}
	if full == nil {
		full = []project.Session{}
	}
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-sessions.json"`, slug))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project": slug, "count": len(full), "sessions": full,
		})
	default:
		var b strings.Builder
		b.WriteString("# Sessions · " + slug + "\n\n")
		b.WriteString(fmt.Sprintf("exported: %s · count: %d\n\n", time.Now().UTC().Format(time.RFC3339), len(full)))
		for _, sess := range full {
			b.WriteString("---\n\n# ")
			b.WriteString(sess.Title)
			b.WriteString(fmt.Sprintf("\n\n- id: `%s`\n- updated: %s\n\n", sess.ID, sess.UpdatedAt.Format(time.RFC3339)))
			for i, t := range sess.Turns {
				role := t.Role
				if role == "" {
					role = "message"
				}
				b.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, role))
				if t.Text != "" {
					b.WriteString(t.Text)
					b.WriteString("\n\n")
				}
			}
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-sessions.md"`, slug))
		_, _ = w.Write([]byte(b.String()))
	}
}

// handleSessionExport returns session as markdown or json (?format=md|json).
func (a *API) handleSessionExport(w http.ResponseWriter, r *http.Request, slug, sid string) {
	sess, err := a.projects.GetSession(slug, sid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "md"
	}
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, sid))
		_ = json.NewEncoder(w).Encode(sess)
	default:
		var b strings.Builder
		b.WriteString("# ")
		b.WriteString(sess.Title)
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("- session_id: `%s`\n- project: `%s`\n- updated: %s\n\n---\n\n",
			sess.ID, slug, sess.UpdatedAt.Format(time.RFC3339)))
		for i, t := range sess.Turns {
			role := t.Role
			if role == "" {
				role = "message"
			}
			b.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, role))
			if t.Mode != "" {
				b.WriteString(fmt.Sprintf("_mode: %s_\n\n", t.Mode))
			}
			if t.Text != "" {
				b.WriteString(t.Text)
				b.WriteString("\n\n")
			}
			for _, tool := range t.Tools {
				b.WriteString(fmt.Sprintf("### tool `%s` (%s)\n\n", tool.Name, tool.Status))
				if tool.Args != "" {
					b.WriteString("```\n" + tool.Args + "\n```\n\n")
				}
				if tool.Output != "" {
					b.WriteString("```\n" + tool.Output + "\n```\n\n")
				}
				if tool.Err != "" {
					b.WriteString("**error:** " + tool.Err + "\n\n")
				}
			}
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, sid))
		_, _ = w.Write([]byte(b.String()))
	}
}

func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := loadLocalConfig(a.sciDir)
		// re-read disk so UI sees latest after PUT without restart (also update a.local)
		a.local = cfg
		model := ""
		if sci, err := scienceConfig(a.sciDir); err == nil {
			if p := sci.ActiveProfile(); p != nil {
				model = p.ID
				if p.Name != "" {
					model = p.Name
				}
			}
		}
		if m := os.Getenv("LUMEN_SCIENCE_MODEL"); m != "" {
			model = m
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"default_mode": cfg.DefaultMode,
			"tool_profile": cfg.ToolProfile,
			"default_port": cfg.DefaultPort,
			"model_hint":   model,
			"version":      a.version,
			"modes":        []string{"agent", "plan", "bypass", "default"},
		})
	case http.MethodPut, http.MethodPost:
		var body struct {
			DefaultMode string `json:"default_mode"`
			ToolProfile string `json:"tool_profile"`
			APIKey      string `json:"api_key"`
			BaseURL     string `json:"base_url"`
			Model       string `json:"model"`
			Provider    string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid body"))
			return
		}
		if a.auth != nil && (body.APIKey != "" || body.BaseURL != "" || body.Model != "" || body.Provider != "") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"code": "provider_config_forbidden", "error": "hosted provider configuration is startup-only"})
			return
		}
		cfg := loadLocalConfig(a.sciDir)
		if body.DefaultMode != "" {
			cfg.DefaultMode = body.DefaultMode
		}
		if body.ToolProfile != "" {
			cfg.ToolProfile = body.ToolProfile
		}
		if err := saveLocalConfig(a.sciDir, cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		a.local = cfg
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "default_mode": cfg.DefaultMode, "tool_profile": cfg.ToolProfile,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSkillsSub routes /api/lab/skills/import
func (a *API) handleSkillsSub(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/api/lab/skills/")
	sub = strings.Trim(sub, "/")
	if sub == "import" {
		a.handleSkillsImport(w, r)
		return
	}
	http.NotFound(w, r)
}

// handleSkillsImport accepts multipart .md or .zip of skills into project .lumen/skills.
func (a *API) handleSkillsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	projects := a.projects
	if tenant := tenantFromRequest(r); tenant != nil {
		projects = tenant.Projects
	}
	if p, err := projects.Get(slug); err == nil {
		slug = p.Slug
	}
	projDir, err := projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(projDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	const destDir = ".lumen/skills"
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("upload too large or bad form"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("需要 file 字段（.md 或 .zip）"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := strings.ToLower(header.Filename)
	var written []string
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid zip"))
			return
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			base := filepath.Base(f.Name)
			if !strings.HasSuffix(strings.ToLower(base), ".md") {
				continue
			}
			if strings.Contains(base, "..") {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
			_ = rc.Close()
			if err := g.WriteFile(filepath.Join(destDir, base), body, 0o600); err == nil {
				written = append(written, base)
			}
		}
	} else {
		// single .md
		base := filepath.Base(header.Filename)
		if !strings.HasSuffix(strings.ToLower(base), ".md") {
			base = base + ".md"
		}
		if err := g.WriteFile(filepath.Join(destDir, base), data, 0o600); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		written = append(written, base)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "imported": written, "count": len(written), "dir": ".lumen/skills",
	})
}

func (a *API) handleSkills(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	switch r.Method {
	case http.MethodGet:
		ws := ""
		if slug != "" {
			if p, err := a.projects.Get(slug); err == nil {
				ws, _ = a.projects.WorkspacePath(p.Slug)
				slug = p.Slug
			}
		}
		home, _ := os.UserHomeDir()
		skillPaths := []string{
			filepath.Join(home, ".lumen", "skills"),
			filepath.Join(home, ".lumen", "imported-skills"),
			filepath.Join(a.sciDir, "skills"),
		}
		if packSkills := labruntime.SkillsDir(a.sciDir); packSkills != "" {
			skillPaths = append(skillPaths, packSkills)
		}
		store := skill.New(skill.Options{
			HomeDir:     home,
			ProjectRoot: ws,
			CustomPaths: skillPaths,
		})
		list := store.List()
		enabled, _ := a.projects.LoadEnabledSkills(slug)
		enSet := map[string]bool{}
		for _, n := range enabled {
			enSet[n] = true
		}
		// nil enabled list → treat all as enabled for UI checkboxes
		allEnabled := len(enabled) == 0
		out := make([]map[string]any, 0, len(list))
		for _, sk := range list {
			en := allEnabled || enSet[sk.Name]
			out = append(out, map[string]any{
				"name":        sk.Name,
				"description": sk.Description,
				"scope":       string(sk.Scope),
				"source":      string(sk.Scope),
				"enabled":     en,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"skills":         out,
			"count":          len(out),
			"enabled":        enabled,
			"enabled_filter": !allEnabled,
		})
	case http.MethodPut, http.MethodPost:
		var body struct {
			ProjectID string   `json:"project_id"`
			Enabled   []string `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid body"))
			return
		}
		if body.ProjectID == "" {
			body.ProjectID = slug
		}
		if body.ProjectID == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
			return
		}
		if p, err := a.projects.Get(body.ProjectID); err == nil {
			body.ProjectID = p.Slug
		}
		if err := a.projects.SaveEnabledSkills(body.ProjectID, body.Enabled); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": body.Enabled})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ProjectID string `json:"project_id"`
		Topic     string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Topic == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("topic required"))
		return
	}
	slug := body.ProjectID
	if p, err := a.projects.Get(slug); err == nil {
		slug = p.Slug
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := brief.Generate(r.Context(), a.sciDir, brief.Request{Topic: body.Topic})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	const briefPath = "reports/brief.md"
	if err := g.WriteFile(briefPath, []byte(res.Markdown), 0o600); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	outPath, _ := g.Resolve(briefPath)
	if projDir, err := a.projects.ProjectDir(slug); err == nil {
		rec, _ := provenance.NewRecorder(projDir, "", os.Getenv("LUMEN_SCIENCE_MODEL"))
		_ = rec.RecordArtifact(outPath)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": "reports/brief.md", "markdown": res.Markdown})
}

func (a *API) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var artifacts []map[string]any
	for _, sub := range []string{"reports", "figures", "data", "notebooks", "imports"} {
		_ = g.Walk(sub, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel := filepath.ToSlash(path)
			artifacts = append(artifacts, map[string]any{
				"path":        rel,
				"name":        filepath.Base(rel),
				"size":        info.Size(),
				"mtime":       info.ModTime().UTC().Format(time.RFC3339),
				"previewKind": previewKind(rel),
				"bucket":      sub,
			})
			return nil
		})
	}
	// newest first
	for i := 1; i < len(artifacts); i++ {
		for j := i; j > 0; j-- {
			mj, _ := artifacts[j]["mtime"].(string)
			mi, _ := artifacts[j-1]["mtime"].(string)
			if mj > mi {
				artifacts[j], artifacts[j-1] = artifacts[j-1], artifacts[j]
			} else {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts, "count": len(artifacts)})
}

func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Long scientific prompts may exceed default middleware body cap.
	r.Body = http.MaxBytesReader(w, r.Body, ChatBodyMaxBytes)
	var req struct {
		ProjectID string `json:"project_id"`
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
		Mode      string `json:"mode"`
		ParentID  string `json:"parent_run_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("prompt required"))
		return
	}
	projects, ctrls := a.projects, a.ctrls
	if tenant := tenantFromRequest(r); tenant != nil {
		projects, ctrls = tenant.Projects, tenant.Controllers
	}
	slug := req.ProjectID
	if p, err := projects.Get(slug); err == nil {
		slug = p.Slug
	} else if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}

	if !a.turns.tryAcquire() {
		w.Header().Set("Retry-After", "2")
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("实验室繁忙（并发上限 %d），请稍后重试", MaxConcurrentTurns))
		return
	}
	defer a.turns.release()

	ctrl, err := ctrls.acquire(slug)
	if err != nil {
		w.Header().Set("Retry-After", "1")
		writeErr(w, http.StatusConflict, err)
		return
	}
	defer ctrls.release(slug)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sse := sseSink{w: w, flusher: flusher}
	hist := newHistorySink(sse)
	mode := req.Mode
	if mode == "" {
		mode = a.local.DefaultMode
	}
	a.setOwnerMode(labOwner(r), mode)

	// Persist session id (create if missing / unknown)
	if sess, err := projects.EnsureSession(slug, req.SessionID, ""); err == nil {
		req.SessionID = sess.ID
		// Tell client the canonical session id early
		sse.emitPayload("session", map[string]any{"id": sess.ID, "title": sess.Title})
		_, _ = projects.AppendTurns(slug, sess.ID, project.Turn{
			Role: "user",
			Text: req.Prompt,
			Mode: mode,
		})
	}

	// Configure with timeout to prevent indefinite hang
	cfgCtx, cfgCancel := context.WithTimeout(r.Context(), ConfigureTimeout)
	defer cfgCancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- fmt.Errorf("configure panic: %v", rec)
			}
		}()
		done <- ctrl.Configure(slug, req.SessionID, hist, a.makeOwnedApprover(labOwner(r), sse.emitPayload))
	}()
	select {
	case err := <-done:
		if err != nil {
			ctrls.discard(slug, ctrl)
			a.turnsFailed.Add(1)
			sse.emit("error", err.Error())
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	case <-cfgCtx.Done():
		ctrls.discard(slug, ctrl)
		a.turnsFailed.Add(1)
		sse.emit("error", "配置超时，请刷新页面重试")
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	owner := labOwner(r)
	if req.ParentID != "" {
		if _, parentErr := a.runs.ValidateRetryParent(owner, req.ParentID); parentErr != nil {
			a.turnsFailed.Add(1)
			sse.emit("error", "parent run must be an owned terminal run")
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	}
	limits := quota.LocalLimits()
	startedAt := time.Now().UTC()
	runID := ""
	var admission quota.Admission
	if a.auth != nil {
		runID, err = runstate.NewRunID()
		if err != nil {
			sse.emit("error", err.Error())
			return
		}
		if a.quota == nil {
			err = &quota.Error{Code: quota.CodeUnavailable, Message: "quota service unavailable", NextAction: "retry_later"}
		} else {
			admission = quota.Admission{RunID: runID, IdempotencyKey: runID + ":admit", Owner: owner, StartedAt: startedAt}
			limits, err = a.quota.Admit(r.Context(), admission)
		}
		if err != nil {
			var qe *quota.Error
			if errors.As(err, &qe) {
				sse.emitPayload("error", map[string]any{"code": qe.Code, "error": qe.Message, "next_action": qe.NextAction})
			} else {
				sse.emit("error", err.Error())
			}
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	}
	var run runstate.Run
	if a.auth != nil {
		run, err = a.runs.StartOwnedID(owner, runID, req.SessionID, "lab", summarizeScienceRunTitle(req.Prompt), req.ParentID, startedAt)
	} else {
		run, err = a.runs.StartOwned(owner, req.SessionID, "lab", summarizeScienceRunTitle(req.Prompt), req.ParentID)
	}
	if err != nil {
		if a.auth != nil && a.quota != nil {
			_ = a.quota.Complete(context.Background(), quota.Completion{RunID: runID, IdempotencyKey: runID + ":complete", Owner: owner, Status: "failed", CompletedAt: time.Now().UTC()})
		}
		a.turnsFailed.Add(1)
		sse.emit("error", err.Error())
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}
	computeStartedAt := time.Now()
	completionStatus := "failed"
	ctrl.SetMaxSteps(limits.MaxSteps)
	pc := ctrl.ProviderConfig()
	pricing := usage.Pricing{}
	providerName, modelName := "", ""
	if pc != nil {
		providerName, modelName = pc.Name, pc.Model
		if pc.Pricing != nil {
			pricing = usage.Pricing{Input: pc.Pricing.Input, Output: pc.Pricing.Output, CacheHit: pc.Pricing.CacheHit}
		}
	}
	timeout := limits.MaxWallTime
	if timeout <= 0 {
		timeout = DefaultTurnTimeout
	}
	ctx, cleanupRun := a.beginActiveRun(r.Context(), owner, run.ID, timeout)
	defer cleanupRun()
	var runFailure quota.Failure
	stopLease := quota.MaintainLease(ctx, a.quota, admission, limits.HeartbeatInterval, func(e error) {
		runFailure.Set(e)
		a.cancelActiveRun(owner, run.ID)
	})
	defer stopLease()
	artifactCapture := &artifact.CapturingSink{Context: ctx, Store: a.artifactStore, Owner: owner, RunID: run.ID, Model: modelName, Workspace: ctrl.WorkspaceContext(), Next: hist, Failure: func(e error) {
		runFailure.Set(fmt.Errorf("artifact persistence failed: %w", e))
		a.cancelActiveRun(owner, run.ID)
	}}
	usageStore := a.usage
	if a.auth != nil {
		usageStore = quota.UsageStore{Usage: a.usage, Quota: a.quota}
	}
	capture := usage.CapturingSink{Store: usageStore, Owner: owner, Provider: providerName, Model: modelName, Pricing: pricing, Next: artifactCapture, Failure: func(e error) {
		runFailure.Set(fmt.Errorf("usage persistence failed: %w", e))
		a.cancelActiveRun(owner, run.ID)
	}}
	limited := &quota.Sink{Limits: limits, Next: a.runs.WrapSink(run.ID, capture), Failure: func(e error) { runFailure.Set(e); a.cancelActiveRun(owner, run.ID) }}
	ctrl.BindRun(run.ID, limited)

	a.turnsTotal.Add(1)
	runErr := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("turn panic: %v", rec)
			}
		}()
		return ctrl.Run(ctx, req.Prompt, mode)
	}()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		runErr = &quota.Error{Code: quota.CodeWallTime, Message: "run wall time quota exceeded", NextAction: "start_new_run"}
	} else if failure := runFailure.Err(); failure != nil {
		runErr = failure
	}
	if runErr == nil {
		completionStatus = "succeeded"
	} else if errors.Is(ctx.Err(), context.Canceled) && runFailure.Err() == nil {
		completionStatus = "canceled"
	}
	stopLease()
	if a.auth != nil && a.quota != nil {
		if completeErr := a.quota.Complete(context.Background(), quota.Completion{RunID: run.ID, IdempotencyKey: run.ID + ":complete", Owner: owner, Status: completionStatus, ComputeMillis: time.Since(computeStartedAt).Milliseconds(), CompletedAt: time.Now().UTC()}); completeErr != nil && runErr == nil {
			runErr = fmt.Errorf("quota completion failed: %w", completeErr)
		}
	}
	if _, finishErr := a.runs.Finish(run.ID, runErr); finishErr != nil {
		if runErr == nil {
			runErr = finishErr
		}
		sse.emit("error", "finish run: "+finishErr.Error())
	}
	if runErr != nil {
		a.turnsFailed.Add(1)
		sse.emit("error", runErr.Error())
	}
	// Persist assistant turn summary
	if req.SessionID != "" {
		text, tools := hist.snapshot()
		if text != "" || len(tools) > 0 || runErr != nil {
			asst := project.Turn{Role: "assistant", Text: text, Tools: tools, Mode: mode}
			if runErr != nil && text == "" {
				asst.Text = runErr.Error()
			}
			_, _ = a.projects.AppendTurns(slug, req.SessionID, asst)
		}
	}
	terminal := map[string]any{"ok": runErr == nil, "run_id": run.ID}
	if runErr != nil {
		terminal["error"] = runErr.Error()
	}
	sse.emitPayload("stream_done", terminal)
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func summarizeScienceRunTitle(prompt string) string {
	title := strings.TrimSpace(prompt)
	const maxRunes = 120
	runes := []rune(title)
	if len(runes) > maxRunes {
		title = string(runes[:maxRunes])
	}
	return title
}

func (a *API) setActiveMode(mode string) {
	var m permission.Mode
	switch mode {
	case "plan", "":
		m = permission.ModePlan
	case "bypass":
		m = permission.ModeBypass
	default:
		m = permission.ModeDefault
	}
	a.modeMu.Lock()
	a.activeMode = m
	a.modeMu.Unlock()
}

func (a *API) setOwnerMode(owner runstate.Owner, mode string) {
	m := parseLabMode(mode)
	a.modeMu.Lock()
	a.ownerModes[owner] = m
	a.modeMu.Unlock()
}

func (a *API) ownerMode(owner runstate.Owner) permission.Mode {
	a.modeMu.Lock()
	defer a.modeMu.Unlock()
	if m, ok := a.ownerModes[owner]; ok {
		return m
	}
	return permission.ModeDefault
}

func (a *API) forgetOwnerMode(owner runstate.Owner) {
	a.modeMu.Lock()
	delete(a.ownerModes, owner)
	a.modeMu.Unlock()
}

func parseLabMode(mode string) permission.Mode {
	switch mode {
	case "bypass", "accept-edits":
		return permission.ModeBypass
	case "plan":
		return permission.ModePlan
	default:
		return permission.ModeDefault
	}
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s sseSink) Emit(e event.Event) {
	data, _ := json.Marshal(e)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) emit(kind, text string) {
	data, _ := json.Marshal(map[string]any{"kind": kind, "text": text})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) emitPayload(kind string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["kind"] = kind
	data, _ := json.Marshal(payload)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

// handleFiles serves workspace file tree, content, and downloads.
func (a *API) handleFiles(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Route sub-paths: /api/lab/files/content?path=, /api/lab/files/download?path=, /api/lab/files/upload, /api/lab/files/search
	sub := strings.TrimPrefix(r.URL.Path, "/api/lab/files")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case sub == "content" || sub == "":
		a.handleFileContent(w, r, g)
	case sub == "download":
		a.handleFileDownload(w, r, g)
	case sub == "upload":
		a.handleFileUpload(w, r, g)
	case sub == "search":
		a.handleFileSearch(w, r, g)
	case sub == "recent":
		a.handleFileRecent(w, r, g)
	case sub == "tree":
		a.handleFileTree(w, r, g)
	case sub == "export":
		a.handleWorkspaceExport(w, r, g)
	case sub == "import":
		a.handleWorkspaceImport(w, r, g)
	case sub == "delete":
		a.handleFilesDelete(w, r, g)
	case sub == "write":
		a.handleFileWrite(w, r, g)
	case sub == "append":
		a.handleFileAppend(w, r, g)
	case sub == "diff":
		a.handleFileDiff(w, r, g)
	case sub == "mkdir":
		a.handleFileMkdir(w, r, g)
	case sub == "rename":
		a.handleFileRename(w, r, g)
	case sub == "copy":
		a.handleFileCopy(w, r, g)
	case sub == "stats":
		a.handleFileStats(w, r, g)
	case sub == "zip":
		a.handleFilesZip(w, r, g)
	default:
		http.NotFound(w, r)
	}
}

// handleFilesZip builds a zip of selected workspace paths (POST {paths:[]}).
func (a *API) handleFilesZip(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("paths required"))
		return
	}
	if len(body.Paths) > 200 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("too many paths (max 200)"))
		return
	}
	slug := r.URL.Query().Get("project_id")
	name := "selection.zip"
	if slug != "" {
		name = slug + "-selection.zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	zw := zip.NewWriter(w)
	defer zw.Close()
	const maxFiles = 2000
	const maxTotal = 64 << 20
	var nFiles int
	var total int64
	seen := map[string]bool{}
	addFile := func(rel string, size int64) error {
		rel = filepath.ToSlash(rel)
		if seen[rel] {
			return nil
		}
		if size > 8<<20 {
			return nil
		}
		if nFiles >= maxFiles || total >= maxTotal {
			return fmt.Errorf("limit")
		}
		data, err := g.ReadFile(rel)
		if err != nil {
			return nil
		}
		fw, err := zw.Create(rel)
		if err != nil {
			return nil
		}
		_, _ = fw.Write(data)
		seen[rel] = true
		nFiles++
		total += int64(len(data))
		return nil
	}
	for _, p := range body.Paths {
		p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
		if p == "" || p == "." || strings.HasPrefix(p, "..") {
			continue
		}
		info, err := g.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			_ = addFile(p, info.Size())
			continue
		}
		// walk directory
		_ = g.Walk(p, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			if nFiles >= maxFiles || total >= maxTotal {
				return filepath.SkipAll
			}
			rel := filepath.ToSlash(path)
			_ = addFile(rel, fi.Size())
			return nil
		})
	}
	if nFiles == 0 {
		// zip already started — write a note file
		fw, err := zw.Create("README.txt")
		if err == nil {
			_, _ = fw.Write([]byte("no files matched selection\n"))
		}
	}
}

// handleFileStats returns lightweight workspace counts for the status pane.
func (a *API) handleFileStats(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var files, dirs int
	var bytes int64
	var newest time.Time
	const maxWalk = 5000
	n := 0
	_ = g.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if n >= maxWalk {
			return filepath.SkipAll
		}
		base := info.Name()
		if base == ".git" || base == "node_modules" || base == ".lumen" {
			if info.IsDir() && path != "." {
				return filepath.SkipDir
			}
		}
		if path == "." {
			return nil
		}
		n++
		if info.IsDir() {
			dirs++
			return nil
		}
		files++
		bytes += info.Size()
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	out := map[string]any{
		"files": files, "dirs": dirs, "bytes": bytes,
		"truncated": n >= maxWalk,
	}
	if !newest.IsZero() {
		out["newest"] = newest.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleFileWrite writes text content into a workspace path.
// POST {path, content}
func (a *API) handleFileWrite(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("path required"))
		return
	}
	rel := filepath.ToSlash(filepath.Clean(body.Path))
	if rel == "." || strings.HasPrefix(rel, "..") {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid path"))
		return
	}
	// cap 8 MiB text writes
	if len(body.Content) > 8<<20 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("content too large"))
		return
	}
	if err := g.WriteFile(rel, []byte(body.Content), 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	size := int64(len(body.Content))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "path": rel, "size": size, "previewKind": previewKind(rel),
	})
}

// handleFileAppend appends text to an existing (or new) workspace file.
// POST {path, content}
func (a *API) handleFileAppend(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("path required"))
		return
	}
	rel := filepath.ToSlash(filepath.Clean(body.Path))
	if rel == "." || strings.HasPrefix(rel, "..") {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid path"))
		return
	}
	if len(body.Content) > 8<<20 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("content too large"))
		return
	}
	// existing size + append must stay under 16 MiB
	var existing int64
	if st, err := g.Stat(rel); err == nil {
		existing = st.Size()
	}
	if existing+int64(len(body.Content)) > 16<<20 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("file would exceed 16MB after append"))
		return
	}
	f, err := g.OpenFile(rel, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	n, err := f.Write([]byte(body.Content))
	_ = f.Close()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	size := existing + int64(n)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "path": rel, "size": size, "appended": n, "previewKind": previewKind(rel),
	})
}

// handleFileDiff returns a simple unified-ish diff between two workspace files.
// GET ?a=&b=
func (a *API) handleFileDiff(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pa := r.URL.Query().Get("a")
	pb := r.URL.Query().Get("b")
	if pa == "" || pb == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("a and b paths required"))
		return
	}
	dataA, err := g.ReadFile(pa)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	dataB, err := g.ReadFile(pb)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	// cap
	const max = 256 * 1024
	truncA, truncB := false, false
	if len(dataA) > max {
		dataA = dataA[:max]
		truncA = true
	}
	if len(dataB) > max {
		dataB = dataB[:max]
		truncB = true
	}
	diffText := simpleLineDiff(pa, pb, string(dataA), string(dataB))
	writeJSON(w, http.StatusOK, map[string]any{
		"a": pa, "b": pb,
		"diff":        diffText,
		"identical":   string(dataA) == string(dataB),
		"truncated_a": truncA,
		"truncated_b": truncB,
	})
}

// simpleLineDiff is a minimal LCS-free line diff for productivity previews.
func simpleLineDiff(nameA, nameB, a, b string) string {
	la := strings.Split(strings.ReplaceAll(a, "\r\n", "\n"), "\n")
	lb := strings.Split(strings.ReplaceAll(b, "\r\n", "\n"), "\n")
	var out strings.Builder
	out.WriteString("--- " + nameA + "\n+++ " + nameB + "\n")
	// Myers-lite: walk with index maps for equal lines (O(n) greedy)
	i, j := 0, 0
	for i < len(la) || j < len(lb) {
		if i < len(la) && j < len(lb) && la[i] == lb[j] {
			out.WriteString(" ")
			out.WriteString(la[i])
			out.WriteByte('\n')
			i++
			j++
			continue
		}
		// look ahead for match
		found := false
		if i < len(la) {
			for k := j; k < len(lb) && k < j+20; k++ {
				if la[i] == lb[k] {
					for j < k {
						out.WriteString("+")
						out.WriteString(lb[j])
						out.WriteByte('\n')
						j++
					}
					found = true
					break
				}
			}
		}
		if !found && j < len(lb) {
			for k := i; k < len(la) && k < i+20; k++ {
				if lb[j] == la[k] {
					for i < k {
						out.WriteString("-")
						out.WriteString(la[i])
						out.WriteByte('\n')
						i++
					}
					found = true
					break
				}
			}
		}
		if !found {
			if i < len(la) {
				out.WriteString("-")
				out.WriteString(la[i])
				out.WriteByte('\n')
				i++
			}
			if j < len(lb) {
				out.WriteString("+")
				out.WriteString(lb[j])
				out.WriteByte('\n')
				j++
			}
		}
	}
	return out.String()
}

// relPathOK rejects empty, absolute, and parent-escape paths.
func relPathOK(p string) (string, error) {
	p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if p == "" || p == "." {
		return "", fmt.Errorf("invalid path")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "..") || strings.Contains(p, "/../") {
		return "", fmt.Errorf("invalid path")
	}
	return p, nil
}

// handleFileMkdir creates a directory under the workspace.
// POST {path}
func (a *API) handleFileMkdir(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := relPathOK(body.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := g.MkdirAll(p, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": p})
}

// handleFileRename renames/moves a workspace path.
// POST {from, to}
func (a *API) handleFileRename(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	from, err := relPathOK(body.From)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("from: %w", err))
		return
	}
	to, err := relPathOK(body.To)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("to: %w", err))
		return
	}
	if _, err := g.Stat(from); err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("source not found: %w", err))
		return
	}
	if _, err := g.Stat(to); err == nil {
		writeErr(w, http.StatusConflict, fmt.Errorf("destination exists: %s", to))
		return
	}
	if err := g.Rename(from, to); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "from": from, "to": to})
}

// handleFileCopy copies a file (or small dir recursively) within the workspace.
// POST {from, to}
func (a *API) handleFileCopy(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	from, err := relPathOK(body.From)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("from: %w", err))
		return
	}
	to, err := relPathOK(body.To)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("to: %w", err))
		return
	}
	info, err := g.Stat(from)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("source not found: %w", err))
		return
	}
	if _, err := g.Stat(to); err == nil {
		writeErr(w, http.StatusConflict, fmt.Errorf("destination exists: %s", to))
		return
	}
	if info.IsDir() {
		var files int
		var bytes int64
		if err := g.Walk(from, func(_ string, st os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if st.IsDir() {
				return nil
			}
			files++
			bytes += st.Size()
			if files > 500 {
				return fmt.Errorf("too many files (max 500)")
			}
			if bytes > 32<<20 {
				return fmt.Errorf("directory too large to copy")
			}
			return nil
		}); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := g.Copy(from, to); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		if info.Size() > 32<<20 {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("file too large to copy (max 32MB)"))
			return
		}
		if err := g.Copy(from, to); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "from": from, "to": to})
}

// handleFilesDelete removes workspace files/dirs (POST {paths:[]}).
func (a *API) handleFilesDelete(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("paths required"))
		return
	}
	if len(body.Paths) > 200 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("too many paths (max 200)"))
		return
	}
	var deleted []string
	var failed []map[string]string
	for _, p := range body.Paths {
		p = filepath.ToSlash(filepath.Clean(p))
		if p == "." || p == "" || strings.HasPrefix(p, "..") {
			failed = append(failed, map[string]string{"path": p, "error": "invalid path"})
			continue
		}
		if err := g.RemoveAll(p); err != nil {
			failed = append(failed, map[string]string{"path": p, "error": err.Error()})
			continue
		}
		deleted = append(deleted, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "deleted": deleted, "count": len(deleted), "failed": failed,
	})
}

// handleWorkspaceImport unpacks a zip into the workspace (multipart field "file").
func (a *API) handleWorkspaceImport(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("zip 过大或格式错误: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("需要 multipart 字段 file（zip）"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid zip: %w", err))
		return
	}
	// optional dest prefix
	destPrefix := strings.TrimSpace(r.FormValue("dest"))
	if destPrefix == "" {
		destPrefix = "imports/upload-" + time.Now().UTC().Format("20060102-150405")
	}
	destPrefix = filepath.ToSlash(filepath.Clean(destPrefix))
	if strings.HasPrefix(destPrefix, "..") || filepath.IsAbs(destPrefix) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid dest"))
		return
	}
	const maxFiles = 2000
	const maxTotal = 64 << 20
	var n int
	var total int64
	var written []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(f.Name))
		if strings.HasPrefix(name, "..") || strings.Contains(name, "..") {
			continue
		}
		if n >= maxFiles || total >= maxTotal {
			break
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		_ = rc.Close()
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(destPrefix, name))
		if err := g.WriteFile(rel, body, 0o600); err != nil {
			continue
		}
		written = append(written, rel)
		n++
		total += int64(len(body))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"files": written,
		"count": len(written),
		"dest":  destPrefix,
		"zip":   header.Filename,
		"bytes": total,
	})
}

// handleWorkspaceExport zips the project workspace (path-filtered) as application/zip.
func (a *API) handleWorkspaceExport(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	name := "workspace.zip"
	if slug != "" {
		name = slug + "-workspace.zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	zw := zip.NewWriter(w)
	defer zw.Close()
	const maxFiles = 5000
	const maxTotal = 64 << 20 // 64 MiB
	var nFiles int
	var total int64
	_ = g.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := info.Name()
		if info.IsDir() {
			if base == ".git" || base == "node_modules" || base == ".lumen" {
				return filepath.SkipDir
			}
			return nil
		}
		if nFiles >= maxFiles || total >= maxTotal {
			return filepath.SkipAll
		}
		rel := filepath.ToSlash(path)
		if info.Size() > 8<<20 {
			return nil // skip huge single files
		}
		data, err := g.ReadFile(rel)
		if err != nil {
			return nil
		}
		fw, err := zw.Create(rel)
		if err != nil {
			return nil
		}
		_, _ = fw.Write(data)
		nFiles++
		total += int64(len(data))
		return nil
	})
}

// handleFileTree returns a nested directory tree (depth-limited).
func (a *API) handleFileTree(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rootRel := r.URL.Query().Get("path")
	if rootRel == "" {
		rootRel = "."
	}
	maxDepth := 4
	if v := r.URL.Query().Get("depth"); v != "" {
		fmt.Sscanf(v, "%d", &maxDepth)
	}
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 8 {
		maxDepth = 8
	}
	tree, err := buildFileTree(g, rootRel, 0, maxDepth, 500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tree": tree, "path": rootRel, "depth": maxDepth})
}

type fileTreeNode struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	IsDir       bool           `json:"isDir"`
	PreviewKind string         `json:"previewKind,omitempty"`
	Size        int64          `json:"size,omitempty"`
	Children    []fileTreeNode `json:"children,omitempty"`
}

func buildFileTree(g *workspace.Guard, rel string, depth, maxDepth, budget int) (fileTreeNode, error) {
	st, err := g.Stat(rel)
	if err != nil {
		return fileTreeNode{}, err
	}
	name := filepath.Base(rel)
	if rel == "." || rel == "" {
		name = "."
	}
	node := fileTreeNode{
		Name: name, Path: filepath.ToSlash(rel), IsDir: st.IsDir(),
	}
	if !st.IsDir() {
		node.Size = st.Size()
		node.PreviewKind = previewKind(name)
		return node, nil
	}
	if depth >= maxDepth || budget <= 0 {
		return node, nil
	}
	entries, err := g.ReadDir(rel)
	if err != nil {
		return node, err
	}
	left := budget
	for _, e := range entries {
		if left <= 0 {
			break
		}
		base := e.Name()
		if base == ".git" || base == "node_modules" {
			continue
		}
		childRel := base
		if rel != "" && rel != "." {
			childRel = filepath.ToSlash(filepath.Join(rel, base))
		}
		child, err := buildFileTree(g, childRel, depth+1, maxDepth, left-1)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, child)
		left--
		if e.IsDir() {
			// children already counted roughly
		}
	}
	return node, nil
}

// handleFileRecent returns files under workspace sorted by mtime desc.
func (a *API) handleFileRecent(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 40
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit <= 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}
	type entry struct {
		Name        string    `json:"name"`
		Path        string    `json:"path"`
		Size        int64     `json:"size"`
		Mtime       time.Time `json:"mtime"`
		PreviewKind string    `json:"previewKind"`
	}
	var all []entry
	_ = g.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				base := info.Name()
				if base == ".git" || base == "node_modules" || base == ".lumen" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel := filepath.ToSlash(path)
		all = append(all, entry{
			Name: filepath.Base(rel), Path: rel, Size: info.Size(),
			Mtime: info.ModTime().UTC(), PreviewKind: previewKind(rel),
		})
		return nil
	})
	// sort by mtime desc
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].Mtime.After(all[j-1].Mtime); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	// JSON-friendly mtime strings
	out := make([]map[string]any, 0, len(all))
	for _, e := range all {
		out = append(out, map[string]any{
			"name": e.Name, "path": e.Path, "size": e.Size,
			"mtime": e.Mtime.Format(time.RFC3339), "previewKind": e.PreviewKind,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out, "count": len(out)})
}

func (a *API) handleFileSearch(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"hits": []any{}, "count": 0, "q": q})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	hits, err := SearchWorkspaceGuarded(g, q, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if hits == nil {
		hits = []FileSearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "count": len(hits), "q": q})
}

func (a *API) handleFileContent(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	rel := r.URL.Query().Get("path")
	if rel == "" || rel == "." {
		a.writeDirListing(w, g, ".")
		return
	}

	st, err := g.Stat(rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if st.IsDir() {
		a.writeDirListing(w, g, rel)
		return
	}

	data, err := g.ReadFile(rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	pk := previewKind(rel)
	// Office Open XML → plain-text extract (not WYSIWYG; download still available)
	if pk == "office" {
		text, okind, oerr := ExtractOfficeText(rel, data, 20000)
		if oerr != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"path":        rel,
				"content":     "",
				"size":        st.Size(),
				"truncated":   false,
				"previewKind": "binary",
				"isDir":       false,
				"error":       oerr.Error(),
				"hint":        "Office 预览失败，请下载查看",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":        rel,
			"content":     text,
			"size":        st.Size(),
			"truncated":   false,
			"previewKind": "office",
			"officeKind":  okind,
			"isDir":       false,
			"hint":        "Office 文本抽取预览（非排版还原）；完整文件请下载",
		})
		return
	}
	maxSize := 512 * 1024
	trunc := len(data) > maxSize
	if trunc {
		data = data[:maxSize]
	}
	// Avoid dumping binary garbage into the chat preview pane
	content := string(data)
	if pk == "binary" || pk == "pdf" {
		content = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":        rel,
		"content":     content,
		"size":        st.Size(),
		"truncated":   trunc,
		"previewKind": pk,
		"isDir":       false,
	})
}

func (a *API) writeDirListing(w http.ResponseWriter, g *workspace.Guard, rel string) {
	entries, err := g.ReadDir(rel)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		name := e.Name()
		path := name
		if rel != "" && rel != "." {
			path = filepath.Join(rel, name)
		}
		entry := map[string]any{
			"name":        name,
			"path":        path,
			"isDir":       e.IsDir(),
			"previewKind": previewKind(name),
		}
		if info != nil && !e.IsDir() {
			entry["size"] = info.Size()
			entry["mtime"] = info.ModTime().UTC().Format(time.RFC3339)
		}
		files = append(files, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "path": rel, "root": "."})
}

func previewKind(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown":
		return "markdown"
	case ".json", ".jsonl", ".yml", ".yaml", ".toml", ".csv", ".tsv", ".txt", ".log", ".py", ".r", ".go", ".js", ".ts", ".html", ".css":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return "image"
	case ".pdf":
		return "pdf"
	case ".docx", ".pptx", ".xlsx":
		return "office"
	case ".doc", ".ppt", ".xls":
		return "binary" // legacy OLE — download only
	case ".pdb", ".cif", ".sdf", ".mol":
		return "molecule"
	default:
		if ext == "" {
			return "text"
		}
		return "binary"
	}
}

func (a *API) handleFileDownload(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	rel := r.URL.Query().Get("path")
	data, err := g.ReadFile(rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(rel)))
	_, _ = w.Write(data)
}

// handleFileUpload accepts multipart file uploads into the project workspace.
func (a *API) handleFileUpload(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Limit upload to 64 MB
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("文件过大或格式错误: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("未选择文件: %w", err))
		return
	}
	defer file.Close()

	// Resolve safe path inside workspace
	dst, err := g.OpenFile(header.Filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dst.Close()
	written, err := io.Copy(dst, file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uploaded": header.Filename,
		"size":     written,
	})
}

// handleProvenance returns provenance.jsonl records for a project.
func (a *API) handleProvenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, guardErr := workspace.NewGuard(projectDir)
	if guardErr != nil {
		writeErr(w, http.StatusInternalServerError, guardErr)
		return
	}
	data, err := g.ReadFile("provenance.jsonl")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"records": []any{}, "count": 0})
		return
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var records []any
	artifactFilter := r.URL.Query().Get("path")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if artifactFilter != "" {
			if p, ok := rec["path"].(string); !ok || p != artifactFilter {
				continue
			}
		}
		records = append(records, rec)
	}
	// Reverse for newest-first
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	truncated := false
	if len(records) > limit {
		records = records[:limit]
		truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records": records, "count": len(records), "truncated": truncated,
	})
}

// ── Compute endpoints ──

func (a *API) handleComputeSSHHosts(w http.ResponseWriter, r *http.Request) {
	// POST /api/lab/compute/ssh-hosts/ping  {alias} or {host}
	if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/ping") || r.URL.Query().Get("action") == "ping" {
		a.handleHostPing(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		local := compute.SSHHost{Alias: "local", Hostname: "local-shell", User: "local"}
		out := []map[string]any{
			{"alias": "local", "hostname": "local-shell", "user": "local", "source": "builtin"},
		}
		// Lab-registered hosts
		if reg, err := LoadRegisteredHosts(a.sciDir); err == nil {
			for _, h := range reg {
				out = append(out, map[string]any{
					"alias": h.Alias, "hostname": h.Hostname, "user": h.User,
					"port": h.Port, "notes": h.Notes, "source": "registry",
				})
			}
		}
		// ~/.ssh/config
		if hosts, err := compute.ParseSSHConfig(); err == nil {
			for _, h := range hosts {
				out = append(out, map[string]any{
					"alias": h.Alias, "hostname": h.Hostname, "user": h.User,
					"port": h.Port, "source": "ssh_config",
				})
			}
		} else {
			writeJSON(w, http.StatusOK, map[string]any{
				"hosts": out, "count": len(out),
				"error": err.Error(),
				"hint":  "无 ~/.ssh/config 时仍可用 local；可在 UI 注册主机",
			})
			return
		}
		_ = local
		writeJSON(w, http.StatusOK, map[string]any{"hosts": out, "count": len(out)})
	case http.MethodPost:
		var body RegisteredHost
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Alias) == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("alias required"))
			return
		}
		list, err := UpsertRegisteredHost(a.sciDir, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hosts": list, "count": len(list)})
	case http.MethodDelete:
		alias := r.URL.Query().Get("alias")
		if alias == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("alias required"))
			return
		}
		list, err := DeleteRegisteredHost(a.sciDir, alias)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hosts": list, "count": len(list)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHostPing checks SSH/local connectivity for a host alias.
// POST { "alias": "local" | "gpu1" } or GET ?alias=
func (a *API) handleHostPing(w http.ResponseWriter, r *http.Request) {
	alias := r.URL.Query().Get("alias")
	if r.Method == http.MethodPost {
		var body struct {
			Alias string `json:"alias"`
			Host  string `json:"host"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Alias != "" {
			alias = body.Alias
		} else if body.Host != "" {
			alias = body.Host
		}
	}
	if alias == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("alias required"))
		return
	}
	start := time.Now()
	ok := false
	msg := ""
	if compute.IsLocalHost(alias) {
		cmd := exec.Command("sh", "-lc", "echo ok")
		out, err := cmd.CombinedOutput()
		ok = err == nil && strings.Contains(string(out), "ok")
		if err != nil {
			msg = err.Error()
		} else {
			msg = "local shell ok"
		}
	} else {
		// Prefer registered hostname if present
		target := alias
		if reg, err := LoadRegisteredHosts(a.sciDir); err == nil {
			for _, h := range reg {
				if h.Alias == alias {
					if h.Hostname != "" {
						target = h.Hostname
						if h.User != "" {
							target = h.User + "@" + h.Hostname
						}
					}
					break
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "StrictHostKeyChecking=accept-new",
			target, "echo ok")
		out, err := cmd.CombinedOutput()
		ok = err == nil && strings.Contains(string(out), "ok")
		if err != nil {
			msg = strings.TrimSpace(string(out) + " " + err.Error())
			if len(msg) > 300 {
				msg = msg[:300] + "…"
			}
		} else {
			msg = "ssh ok"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alias":      alias,
		"ok":         ok,
		"message":    msg,
		"latency_ms": time.Since(start).Milliseconds(),
	})
}

func (a *API) handleComputeJobs(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store, err := compute.NewStore(projectDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		jobs, err := store.List()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
	case http.MethodPost:
		var body struct {
			Host        string   `json:"host"`
			Command     string   `json:"command"`
			WorkDir     string   `json:"work_dir"`
			TimeoutSec  int      `json:"timeout_sec"`
			OutputGlobs []string `json:"output_globs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Host == "" || body.Command == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("host and command required"))
			return
		}
		ws, _ := a.projects.WorkspacePath(slug)
		workDir := body.WorkDir
		if workDir == "" {
			workDir = ws
		}
		opts := compute.SubmitOpts{
			OutputGlobs: body.OutputGlobs,
		}
		if body.TimeoutSec > 0 {
			opts.Timeout = time.Duration(body.TimeoutSec) * time.Second
		}
		// Harvest into project .lumen/compute/outputs
		if len(body.OutputGlobs) > 0 {
			if pd, err := a.projects.ProjectDir(slug); err == nil {
				opts.LocalHarvestDir = filepath.Join(pd, ".lumen", "compute", "outputs")
			}
		}
		j, err := store.SubmitOpts(body.Host, body.Command, workDir, opts)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, j)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleComputeJob(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/lab/compute/jobs/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	slug := r.URL.Query().Get("project_id")
	if slug == "" || id == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id and job id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store, err := compute.NewStore(projectDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// POST .../jobs/:id/import — copy harvested output(s) into workspace
	if len(parts) == 2 && parts[1] == "import" && r.Method == http.MethodPost {
		a.handleComputeImport(w, r, slug, projectDir, store, id)
		return
	}
	// POST .../jobs/:id/cancel
	if len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost {
		j, err := store.Cancel(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, j)
		return
	}
	// GET .../jobs/:id/log — SSE tail of job.Output until terminal status
	if len(parts) == 2 && parts[1] == "log" && r.Method == http.MethodGet {
		a.handleComputeJobLog(w, r, store, id)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	j, err := store.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// handleComputeJobLog streams job output as SSE for live log UI.
func (a *API) handleComputeJobLog(w http.ResponseWriter, r *http.Request, store *compute.Store, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	lastLen := -1
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(30 * time.Minute)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			fmt.Fprintf(w, "event: done\ndata: {\"reason\":\"timeout\"}\n\n")
			flusher.Flush()
			return
		case <-ticker.C:
			j, err := store.Get(id)
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", err.Error())
				flusher.Flush()
				return
			}
			out := j.Output
			if len(out) != lastLen {
				lastLen = len(out)
				payload, _ := json.Marshal(map[string]any{
					"id": j.ID, "status": j.Status, "output": out,
					"output_truncated": j.OutputTruncated, "error": j.Error,
				})
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			}
			switch j.Status {
			case "done", "failed", "timeout", "cancelled":
				fmt.Fprintf(w, "event: done\ndata: {\"status\":%q}\n\n", j.Status)
				flusher.Flush()
				return
			}
		}
	}
}

// handleComputeImport copies one or all job outputs into workspace/imports/<jobid>/.
// Body: { "path": "out.dat" } or { "all": true }
func (a *API) handleComputeImport(w http.ResponseWriter, r *http.Request, slug, projectDir string, store *compute.Store, jobID string) {
	var body struct {
		Path string `json:"path"`
		All  bool   `json:"all"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	j, err := store.Get(jobID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if !body.All && strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("path required or all=true"))
		return
	}
	var paths []string
	if body.All {
		for _, o := range j.Outputs {
			if o.Path != "" && o.Error == "" {
				paths = append(paths, o.Path)
			}
		}
		if len(paths) == 0 && j.WorkDir != "" {
			// nothing harvested metadata — nothing to do
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": []any{}, "count": 0, "job_id": jobID})
			return
		}
	} else {
		paths = []string{body.Path}
	}
	var imported []map[string]any
	var lastErr error
	for _, p := range paths {
		rel, size, err := a.copyJobOutputToWorkspace(slug, projectDir, j, jobID, p)
		if err != nil {
			lastErr = err
			imported = append(imported, map[string]any{"path": p, "error": err.Error()})
			continue
		}
		imported = append(imported, map[string]any{
			"path":           p,
			"workspace_path": rel,
			"size":           size,
			"previewKind":    previewKind(rel),
		})
	}
	if len(imported) == 0 && lastErr != nil {
		writeErr(w, http.StatusNotFound, lastErr)
		return
	}
	// single-path response shape for backward compat
	resp := map[string]any{
		"ok":       true,
		"imported": imported,
		"count":    len(imported),
		"job_id":   jobID,
	}
	if !body.All && len(imported) == 1 {
		if wp, ok := imported[0]["workspace_path"].(string); ok {
			resp["workspace_path"] = wp
			resp["size"] = imported[0]["size"]
			resp["previewKind"] = imported[0]["previewKind"]
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) copyJobOutputToWorkspace(slug, projectDir string, j *compute.Job, jobID, outPath string) (rel string, size int64, err error) {
	var src string
	for _, o := range j.Outputs {
		if o.Path == outPath || filepath.Base(o.Path) == filepath.Base(outPath) {
			if o.LocalPath != "" {
				src = o.LocalPath
			}
			size = o.Size
			break
		}
	}
	if src == "" && j.WorkDir != "" {
		src = filepath.Join(j.WorkDir, outPath)
	}
	if src == "" {
		return "", 0, fmt.Errorf("output not found or not harvested: %s", outPath)
	}
	var data []byte
	for _, root := range []string{projectDir, j.WorkDir} {
		if root == "" {
			continue
		}
		relSrc, relErr := filepath.Rel(root, src)
		if relErr != nil || relSrc == ".." || strings.HasPrefix(relSrc, ".."+string(os.PathSeparator)) || filepath.IsAbs(relSrc) {
			continue
		}
		srcGuard, guardErr := workspace.NewGuard(root)
		if guardErr != nil {
			continue
		}
		data, err = srcGuard.ReadFile(relSrc)
		if err == nil {
			break
		}
	}
	if err != nil || data == nil {
		return "", 0, fmt.Errorf("source outside project or unsafe: %s", outPath)
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		return "", 0, err
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		return "", 0, err
	}
	rel = filepath.ToSlash(filepath.Join("imports", jobID, filepath.Base(src)))
	if err := g.WriteFile(rel, data, 0o600); err != nil {
		return "", 0, err
	}
	if size == 0 {
		size = int64(len(data))
	}
	return rel, size, nil
}

// ── C2D + Bridge endpoints ──

func (a *API) handleC2DAlgorithms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		DatasetID string `json:"dataset_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	_ = a.fleet.ConnectAll()
	text, err := a.fleet.CallNative("c2d", "list_algorithms", map[string]any{
		"dataset_id": strings.TrimSpace(body.DatasetID),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(text), &payload)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": payload})
}

func (a *API) handleBridgeOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"bridge_url":  "http://127.0.0.1:18990",
		"sandbox_url": "http://127.0.0.1:8990",
		"hint":        "在 Bridge 面板中点击「一键开始」启动沙箱，或运行 lumen science start",
	})
}

// ── Notebook / Jupyter endpoints ──

func (a *API) handleNotebooks(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	sub := strings.TrimPrefix(r.URL.Path, "/api/lab/notebooks")
	sub = strings.TrimPrefix(sub, "/")

	switch r.Method {
	case http.MethodGet:
		if sub == "" || sub == "list" {
			notebooks, err := jupyter.ListNotebooksGuarded(g)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"notebooks":         notebooks,
				"count":             len(notebooks),
				"jupyter_available": jupyter.IsAvailable(),
			})
			return
		}
		// Get cell content
		name, err := notebookName(strings.TrimPrefix(sub, "cells/"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		nb, err := jupyter.LoadGuarded(g, filepath.Join("notebooks", name))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":     name,
			"cells":    nb.Cells,
			"count":    len(nb.Cells),
			"markdown": nb.ToMarkdown(),
		})
	case http.MethodPost:
		var body struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if sub == "" || sub == "create" {
			// Create new notebook
			if body.Name == "" {
				body.Name = fmt.Sprintf("notebook_%s.ipynb", time.Now().Format("20060102-150405"))
			}
			if !strings.HasSuffix(body.Name, ".ipynb") {
				body.Name += ".ipynb"
			}
			name, err := notebookName(body.Name)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			body.Name = name
			nb := jupyter.New(strings.TrimSuffix(body.Name, ".ipynb"))
			rel := filepath.ToSlash(filepath.Join("notebooks", body.Name))
			nb.Normalize()
			data, err := json.MarshalIndent(nb, "", "  ")
			if err == nil {
				data = append(data, '\n')
				err = g.WriteFile(rel, data, 0o600)
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"name": body.Name, "path": rel})
			return
		}
		if strings.HasPrefix(sub, "execute/") {
			// Execute notebook
			name, err := notebookName(strings.TrimPrefix(sub, "execute/"))
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			rel := filepath.Join("notebooks", name)
			nb, err := jupyter.LoadGuarded(g, rel)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			python := labruntime.ResolvePython(paths.DataDir(a.sciDir))
			if err := nb.ExecuteGuarded(g, rel, python); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "cells": nb.Cells})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cells": nb.Cells})
			return
		}
		// Append cell
		if strings.HasPrefix(sub, "cell/") {
			name, err := notebookName(strings.TrimPrefix(sub, "cell/"))
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			rel := filepath.Join("notebooks", name)
			nb, err := jupyter.LoadGuarded(g, rel)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			if body.Source != "" {
				nb.AddCode(body.Source)
			}
			if err := nb.SaveGuarded(g, rel); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cells": len(nb.Cells)})
			return
		}
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func notebookName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\\`) || !strings.HasSuffix(name, ".ipynb") {
		return "", fmt.Errorf("invalid notebook name")
	}
	return name, nil
}

// ── LangGraph sidecar ──

func (a *API) handleLangGraphRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req langgraph.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid request body: " + err.Error()})
		return
	}
	req.Provider = a.langGraphProvider()
	// Resolve workspace from project slug when client did not supply an absolute path.
	// project_id is the project slug (frontend activeProject.slug), not proj_* id.
	if strings.TrimSpace(req.Workspace) == "" && strings.TrimSpace(req.ProjectID) != "" && a.projects != nil {
		if ws, err := a.projects.WorkspacePath(req.ProjectID); err == nil {
			req.Workspace = ws
		}
		// invalid slug: leave Workspace empty; runner reports no workspace
	}
	resp := langgraph.Run(r.Context(), req)
	// Infer mode from result text if runner did not set it
	if resp.Mode == "" {
		if strings.Contains(resp.Result, "模式: llm") || strings.Contains(resp.Result, "（LLM）") {
			resp.Mode = "llm"
		} else if resp.OK {
			resp.Mode = "heuristic"
		}
	}
	// Persist server-side history per project (best-effort).
	if a.projects != nil && strings.TrimSpace(req.ProjectID) != "" {
		if pdir, err := a.projects.ProjectDir(req.ProjectID); err == nil {
			_ = langgraph.AppendHistory(pdir, langgraph.HistoryEntry{
				ProjectID: req.ProjectID,
				Prompt:    req.Prompt,
				OK:        resp.OK,
				Result:    resp.Result,
				Error:     resp.Error,
				Mode:      resp.Mode,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) langGraphProvider() *langgraph.ProviderConfig {
	if a.auth != nil {
		if a.platformProvider == nil {
			return nil
		}
		pc := *a.platformProvider
		return &langgraph.ProviderConfig{APIKey: pc.APIKey, BaseURL: pc.BaseURL, Model: pc.Model, Adapter: pc.Kind}
	}
	if sciCfg, err := scienceConfig(a.sciDir); err == nil {
		if pc, _, adapter, err := ScienceProviderConfig(sciCfg); err == nil {
			return &langgraph.ProviderConfig{APIKey: pc.APIKey, BaseURL: pc.BaseURL, Model: pc.Model, Adapter: adapter}
		}
	}
	return nil
}

func (a *API) langGraphHealth() map[string]any {
	if a.auth != nil {
		return langgraph.HealthWithProvider(a.langGraphProvider())
	}
	return langgraph.Health()
}

// handleLangGraphHistory lists server-persisted sidecar runs for a project.
// GET ?project_id=&limit=40
func (a *API) handleLangGraphHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "project_id required"})
		return
	}
	limit := 40
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	pdir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid project"})
		return
	}
	list, err := langgraph.ListHistory(pdir, limit)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(list), "entries": list})
}

// ── OnlyOffice callback + session ──

func (a *API) handleOnlyOfficeCallback(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	relPath := onlyoffice.CallbackPath(r.URL.Query().Get("path"))
	if slug == "" || relPath == "" {
		// OnlyOffice expects HTTP 200 + {"error":N}
		onlyoffice.WriteCB(w, 1)
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		onlyoffice.WriteCB(w, 1)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		onlyoffice.WriteCB(w, 1)
		return
	}
	onlyoffice.HandleCallback(w, r, g, relPath)
}

// handleOnlyOfficeSession returns editor parameters including a server-minted
// callback URL that embeds LUMEN_ONLYOFFICE_CALLBACK_TOKEN when set.
// GET ?project_id=&path=&mode=view|edit
func (a *API) handleOnlyOfficeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.TrimSpace(r.URL.Query().Get("project_id"))
	path := r.URL.Query().Get("path")
	mode := r.URL.Query().Get("mode")
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, onlyoffice.Session{OK: false, Error: "project_id required"})
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, onlyoffice.Session{OK: false, Error: "invalid project"})
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, onlyoffice.Session{OK: false, Error: "workspace unavailable"})
		return
	}
	base := onlyoffice.PublicBaseFromRequest(r)
	if base == "" {
		// Fall back to local listen address
		base = fmt.Sprintf("http://127.0.0.1:%d", a.listenPort)
	}
	sess := onlyoffice.BuildSessionGuarded(base, slug, path, mode, g)
	if !sess.OK {
		writeJSON(w, http.StatusBadRequest, sess)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}
