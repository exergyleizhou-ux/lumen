package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lumen/internal/runstate"
	"lumen/internal/usage"
)

func TestHTTPStoreContractAndMachineAuthentication(t *testing.T) {
	secret := "01234567890123456789012345678901"
	var actions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		actions = append(actions, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		owner, _ := body["owner"].(map[string]any)
		if owner["account_id"] != "user" || owner["workspace_id"] != "workspace" {
			t.Errorf("owner=%v", owner)
		}
		switch {
		case r.URL.Path == "/api/v1/workbench/runtime/quota/runs/run_1/admit":
			_, _ = w.Write([]byte(`{"data":{"quota":{"user_concurrent_runs":2,"workspace_concurrent_runs":4,"monthly_tokens":100,"monthly_compute_millis":200,"storage_bytes":300,"artifact_total_bytes":250,"artifact_single_bytes":50,"run_wall_millis":60000,"run_max_steps":10,"run_max_events":20,"event_max_bytes":4096},"lease_expires_at":"2099-01-01T00:00:00Z"}}`))
		case r.URL.Path == "/api/v1/workbench/runtime/quota/runs/run_1/usage":
			u := body["usage"].(map[string]any)
			if u["cache_read_tokens"] != float64(3) || u["cache_write_tokens"] != float64(4) || u["cost_microunits"] != float64(9) {
				t.Errorf("usage=%v", u)
			}
			_, _ = w.Write([]byte(`{"recorded":true}`))
		case r.URL.Path == "/api/v1/workbench/runtime/quota/runs/run_1/heartbeat":
			_, _ = w.Write([]byte(`{"lease_expires_at":"2099-01-01T00:00:00Z"}`))
		default:
			_, _ = w.Write([]byte(`{"released":true}`))
		}
	}))
	defer srv.Close()

	store, err := NewHTTPStore(srv.URL, secret, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	o := runstate.Owner{UserID: "user", WorkspaceID: "workspace"}
	limits, err := store.Admit(context.Background(), Admission{RunID: "run_1", Owner: o, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if limits.MaxWallTime != time.Minute || limits.MaxSteps != 10 || limits.StorageBytes != 250 {
		t.Fatalf("limits=%+v", limits)
	}
	if err := store.RecordUsage(context.Background(), usage.Record{RunID: "run_1", EventID: "event_1", UserID: o.UserID, WorkspaceID: o.WorkspaceID, CacheHitTokens: 3, CacheMissTokens: 4, EstimatedCostMicros: 9, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(context.Background(), Completion{RunID: "run_1", Owner: o, Status: "succeeded", CompletedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a := Artifact{RunID: "run_1", IdempotencyKey: "artifact_1", Owner: o, Bytes: 12}
	if err := store.ReserveArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := store.Heartbeat(context.Background(), Admission{RunID: "run_1", Owner: o}); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 7 || actions[3] != "/api/v1/workbench/runtime/quota/runs/run_1/artifacts/reserve" || actions[4] != "/api/v1/workbench/runtime/quota/runs/run_1/artifacts/commit" || actions[5] != "/api/v1/workbench/runtime/quota/runs/run_1/artifacts/release" || actions[6] != "/api/v1/workbench/runtime/quota/runs/run_1/heartbeat" {
		t.Fatalf("actions=%v", actions)
	}
}

func TestHTTPStoreTreatsConfiguredURLAsControlPlaneOrigin(t *testing.T) {
	secret := "01234567890123456789012345678901"
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"data":{"quota":{"user_concurrent_runs":2,"workspace_concurrent_runs":2,"monthly_tokens":100,"monthly_compute_millis":100,"storage_bytes":100,"artifact_total_bytes":100,"artifact_single_bytes":50,"run_wall_millis":60000,"run_max_steps":10,"run_max_events":20,"event_max_bytes":4096},"lease_expires_at":"2099-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()
	store, err := NewHTTPStore(srv.URL+"/api/v1/workbench/runtime?legacy=1#fragment", secret, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Admit(context.Background(), Admission{RunID: "run", Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/workbench/runtime/quota/runs/run/admit" {
		t.Fatalf("path=%q", path)
	}
}

func TestHTTPStoreRetriesTransientCompletion(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"released":true}`))
	}))
	defer srv.Close()
	store, _ := NewHTTPStore(srv.URL, "01234567890123456789012345678901", srv.Client())
	err := store.Complete(context.Background(), Completion{RunID: "run", Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, Status: "failed", CompletedAt: time.Now()})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestHTTPStoreReturnsStableLimitErrorAndFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"quota_monthly_tokens","message":"quota exceeded","next_action":"retry_next_month"}}`))
	}))
	defer srv.Close()
	store, _ := NewHTTPStore(srv.URL, "01234567890123456789012345678901", srv.Client())
	_, err := store.Admit(context.Background(), Admission{RunID: "run_1", Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}})
	qerr, ok := err.(*Error)
	if !ok || qerr.Code != CodeTokens || qerr.NextAction != "retry_next_month" {
		t.Fatalf("err=%#v", err)
	}

	srv.Close()
	if err := store.RecordUsage(context.Background(), usage.Record{RunID: "run_1"}); err == nil {
		t.Fatal("transport failure must not be accepted")
	}
}
