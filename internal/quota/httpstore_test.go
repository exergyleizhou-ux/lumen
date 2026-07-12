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
			_, _ = w.Write([]byte(`{"data":{"quota":{"user_concurrent_runs":2,"workspace_concurrent_runs":4,"monthly_tokens":100,"monthly_compute_millis":200,"storage_bytes":300,"artifact_total_bytes":250,"artifact_single_bytes":50,"run_wall_millis":60000,"run_max_steps":10,"run_max_events":20,"event_max_bytes":4096}}}`))
		case r.URL.Path == "/api/v1/workbench/runtime/quota/runs/run_1/usage":
			u := body["usage"].(map[string]any)
			if u["cache_read_tokens"] != float64(3) || u["cache_write_tokens"] != float64(4) || u["cost_microunits"] != float64(9) {
				t.Errorf("usage=%v", u)
			}
			_, _ = w.Write([]byte(`{"recorded":true}`))
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
	if err := store.ReleaseArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 5 || actions[3] != "/api/v1/workbench/runtime/quota/runs/run_1/artifacts/reserve" || actions[4] != "/api/v1/workbench/runtime/quota/runs/run_1/artifacts/release" {
		t.Fatalf("actions=%v", actions)
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
