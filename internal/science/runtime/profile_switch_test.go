package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sciconfig "lumen/internal/science/config"
)

func TestProbeProfileKeyRejects401(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := sciconfig.Profile{
		ID: "p1", Name: "bad", TemplateID: "deepseek", APIKey: "sk-bad",
	}
	cfg := sciconfig.Default()
	cfg.SchemaVersion = sciconfig.CurrentSchemaVersion
	cfg.Profiles = []sciconfig.Profile{p}
	cfg.ActiveProfileID = p.ID
	if err := sciconfig.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	// Override deepseek URL via profile base_url pointing to mock
	_, err := sciconfig.Update(dir, func(c *sciconfig.File) {
		c.Profiles[0].BaseURL = srv.URL
	})
	if err != nil {
		t.Fatal(err)
	}

	ok, hint, err := ProbeProfileKey(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected reject")
	}
	if hint == "" {
		t.Fatal("want hint")
	}
	_ = context.Background()
	_ = json.Marshal
}
