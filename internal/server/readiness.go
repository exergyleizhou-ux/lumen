package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type readinessResult struct {
	Ready  bool            `json:"ready"`
	Checks map[string]bool `json:"checks"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]bool{"process": true}
	if s.cfg.Hosted {
		checks["database"] = s.readinessDB != nil && s.readinessDB.PingContext(ctx) == nil
		checks["object_storage"] = writableDirectory(s.readinessObject)
		checks["quota_control_plane"] = controlPlaneReady(ctx, os.Getenv("WORKBENCH_CONTROL_PLANE_URL"))
		checks["provider"] = s.hostedDefault != nil && strings.TrimSpace(s.hostedDefault.APIKey) != ""
	}
	ready := true
	for _, ok := range checks {
		ready = ready && ok
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(readinessResult{Ready: ready, Checks: checks})
}

func writableDirectory(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	f, err := os.CreateTemp(filepath.Clean(root), ".ready-*")
	if err != nil {
		return false
	}
	name := f.Name()
	ok := f.Close() == nil
	if os.Remove(name) != nil {
		ok = false
	}
	return ok
}

func controlPlaneReady(ctx context.Context, raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/readyz", "", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode >= 200 && res.StatusCode < 300
}
