package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lumen/internal/runstate"
	"lumen/internal/usage"
)

// HTTPStore uses Oasis's machine-authenticated quota transaction boundary.
// It deliberately does not fall back to local counters on transport errors.
type HTTPStore struct {
	base   string
	secret string
	client *http.Client
}

func NewHTTPStore(baseURL, secret string, client *http.Client) (*HTTPStore, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("valid WORKBENCH_CONTROL_PLANE_URL required")
	}
	if len(secret) < 32 {
		return nil, errors.New("WORKBENCH_RUNTIME_INGEST_SECRET must be at least 32 bytes")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPStore{base: strings.TrimRight(u.String(), "/"), secret: secret, client: client}, nil
}

type ownerJSON struct {
	AccountID   string `json:"account_id"`
	WorkspaceID string `json:"workspace_id"`
}

func owner(o runstate.Owner) ownerJSON {
	return ownerJSON{AccountID: o.UserID, WorkspaceID: o.WorkspaceID}
}

type policyJSON struct {
	UserConcurrent       int   `json:"user_concurrent_runs"`
	WorkspaceConcurrent  int   `json:"workspace_concurrent_runs"`
	MonthlyTokens        int64 `json:"monthly_tokens"`
	MonthlyComputeMillis int64 `json:"monthly_compute_millis"`
	StorageBytes         int64 `json:"storage_bytes"`
	ArtifactTotalBytes   int64 `json:"artifact_total_bytes"`
	ArtifactSingleBytes  int64 `json:"artifact_single_bytes"`
	RunWallMillis        int64 `json:"run_wall_millis"`
	RunMaxSteps          int   `json:"run_max_steps"`
	RunMaxEvents         int   `json:"run_max_events"`
	EventMaxBytes        int64 `json:"event_max_bytes"`
}

func (s *HTTPStore) Admit(ctx context.Context, a Admission) (Limits, error) {
	var out struct {
		Data struct {
			Quota policyJSON `json:"quota"`
		} `json:"data"`
	}
	if err := s.post(ctx, a.RunID, "admit", struct {
		Owner     ownerJSON `json:"owner"`
		StartedAt time.Time `json:"started_at"`
	}{owner(a.Owner), a.StartedAt}, &out); err != nil {
		return Limits{}, err
	}
	p := out.Data.Quota
	limits := Limits{UserConcurrent: p.UserConcurrent, WorkspaceConcurrent: p.WorkspaceConcurrent, MonthlyTokens: p.MonthlyTokens, MonthlyComputeMillis: p.MonthlyComputeMillis, StorageBytes: p.StorageBytes, MaxWallTime: time.Duration(p.RunWallMillis) * time.Millisecond, MaxSteps: p.RunMaxSteps, MaxEvents: p.RunMaxEvents, MaxEventBytes: p.EventMaxBytes, MaxArtifactBytes: p.ArtifactSingleBytes}
	if p.ArtifactTotalBytes > 0 && (limits.StorageBytes <= 0 || p.ArtifactTotalBytes < limits.StorageBytes) {
		limits.StorageBytes = p.ArtifactTotalBytes
	}
	if limits.MaxWallTime <= 0 || limits.MaxSteps <= 0 || limits.MaxEvents <= 0 || limits.MaxEventBytes <= 0 || limits.MaxArtifactBytes <= 0 {
		return Limits{}, errors.New("quota service returned invalid runtime limits")
	}
	return limits, nil
}

func (s *HTTPStore) RecordUsage(ctx context.Context, r usage.Record) error {
	type usageJSON struct {
		EventID          string    `json:"event_id"`
		Provider         string    `json:"provider"`
		Model            string    `json:"model"`
		InputTokens      int       `json:"input_tokens"`
		OutputTokens     int       `json:"output_tokens"`
		CacheReadTokens  int       `json:"cache_read_tokens"`
		CacheWriteTokens int       `json:"cache_write_tokens"`
		CostMicrounits   int64     `json:"cost_microunits"`
		ComputeMillis    int64     `json:"compute_millis"`
		OccurredAt       time.Time `json:"occurred_at"`
	}
	return s.post(ctx, r.RunID, "usage", struct {
		Owner ownerJSON `json:"owner"`
		Usage any       `json:"usage"`
	}{owner(runstate.Owner{UserID: r.UserID, WorkspaceID: r.WorkspaceID}), usageJSON{r.EventID, r.Provider, r.Model, r.InputTokens, r.OutputTokens, r.CacheHitTokens, r.CacheMissTokens, r.EstimatedCostMicros, 0, r.CreatedAt}}, nil)
}

func (s *HTTPStore) ReserveArtifact(ctx context.Context, a Artifact) error {
	return s.postPath(ctx, a.RunID, "artifacts/reserve", struct {
		Owner      ownerJSON `json:"owner"`
		ArtifactID string    `json:"artifact_id"`
		SizeBytes  int64     `json:"size_bytes"`
	}{owner(a.Owner), a.IdempotencyKey, a.Bytes}, nil)
}
func (s *HTTPStore) ReleaseArtifact(ctx context.Context, a Artifact) error {
	return s.postPath(ctx, a.RunID, "artifacts/release", struct {
		Owner      ownerJSON `json:"owner"`
		ArtifactID string    `json:"artifact_id"`
	}{owner(a.Owner), a.IdempotencyKey}, nil)
}

func (s *HTTPStore) Complete(ctx context.Context, c Completion) error {
	status := c.Status
	if status == "succeeded" {
		status = "completed"
	}
	return s.post(ctx, c.RunID, "complete", struct {
		Owner       ownerJSON `json:"owner"`
		Status      string    `json:"status"`
		CompletedAt time.Time `json:"completed_at"`
	}{owner(c.Owner), status, c.CompletedAt}, nil)
}

func (s *HTTPStore) post(ctx context.Context, runID, action string, body, out any) error {
	return s.postPath(ctx, runID, action, body, out)
}

func (s *HTTPStore) postPath(ctx context.Context, runID, action string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := s.base + "/api/v1/workbench/runtime/quota/runs/" + url.PathEscape(runID) + "/" + action
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("quota service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code       string `json:"code"`
				Message    string `json:"message"`
				NextAction string `json:"next_action"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope)
		if resp.StatusCode == http.StatusTooManyRequests && envelope.Error.Code != "" {
			return &Error{Code: envelope.Error.Code, Message: envelope.Error.Message, NextAction: envelope.Error.NextAction}
		}
		return fmt.Errorf("quota service returned HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return fmt.Errorf("decode quota response: %w", err)
		}
	}
	return nil
}
