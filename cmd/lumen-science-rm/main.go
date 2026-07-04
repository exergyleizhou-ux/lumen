// lumen-science-rm runs real-machine RM steps in an isolated guard HOME.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/config"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/guard"
	"lumen/internal/science/gui"
	"lumen/internal/science/launcher"
	"lumen/internal/science/oauth"
	"lumen/internal/science/paths"
	"lumen/internal/science/proxy"
	"lumen/internal/science/runtime"
)

func pickPorts() (proxy, sandbox, gui int) {
	proxy = freePort()
	sandbox = freePort()
	for sandbox == proxy {
		sandbox = freePort()
	}
	gui = freePort()
	for gui == proxy || gui == sandbox {
		gui = freePort()
	}
	return proxy, sandbox, gui
}

func freePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 29191
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "RM-manual-auto FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ RM-manual-auto PASS")
}

func run() error {
	sciDir, err := sciconfig.Dir()
	if err != nil {
		return err
	}
	_ = os.MkdirAll(sciDir, 0o700)

	baseline8765 := pidOnPort(8765)
	fmt.Printf("▸ baseline 8765 PID: %s\n", baseline8765)

	lumenCfg, _ := config.LoadWithEnv(config.FindConfig(), config.FindDotEnv())
	key, mockUpstream, upstreamMode := resolveUpstreamKey(lumenCfg)
	if mockUpstream != nil {
		defer mockUpstream.Close()
	}
	fmt.Printf("▸ upstream key mode: %s\n", upstreamMode)

	cfg, err := sciconfig.Load(sciDir)
	if err != nil {
		return err
	}
	proxyPort, sandboxPort, guiPort := pickPorts()
	fmt.Printf("▸ ports proxy=%d sandbox=%d gui=%d\n", proxyPort, sandboxPort, guiPort)
	cfg.ProxyPort = proxyPort
	cfg.SandboxPort = sandboxPort
	cfg.Provider = "deepseek"
	cfg.Providers = map[string]sciconfig.ProviderCfg{"deepseek": {Key: key}}
	cfg.ToolUseShim = "rewrite"
	if mockUpstream != nil {
		cfg.Profiles = []sciconfig.Profile{{
			ID: "rm-live", Name: "RM Live", TemplateID: "custom",
			APIKey: key, BaseURL: mockUpstream.URL,
		}}
		cfg.ActiveProfileID = "rm-live"
	}
	if err := sciconfig.Save(sciDir, cfg); err != nil {
		return err
	}

	mgr, err := runtime.New(sciDir, lumenCfg)
	if err != nil {
		return err
	}

	fmt.Println("▸ RM-04 virtual OAuth sandbox login")
	url, action, err := startSandboxWithRetry(mgr, 4)
	if err != nil {
		return fmt.Errorf("RM-04: %w", err)
	}
	fmt.Printf("  sandbox %s url=%s\n", action, url)
	dataDir := paths.DataDir(sciDir)
	if !oauth.IsLoginIntact(dataDir) {
		return fmt.Errorf("RM-04: login not intact")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "encryption.key")); err != nil {
		return fmt.Errorf("RM-04: missing encryption.key")
	}

	fmt.Println("▸ RM-13 OAuth token reuse")
	sbx := paths.SandboxHome(sciDir)
	realDir, _ := guard.RealScienceDir()
	fr, act, err := oauth.EnsureVirtualLogin(dataDir, sbx, realDir)
	if err != nil {
		return fmt.Errorf("RM-13: %w", err)
	}
	if act != oauth.ActionReused {
		return fmt.Errorf("RM-13: expected reused, got %s", act)
	}
	fmt.Printf("  org=%s action=%s\n", fr.OrgUUID, act)

	secret, err := mgr.EnsureSecret()
	if err != nil {
		return err
	}

	fmt.Println("▸ RM-05 proxy chat round-trip")
	ok, hint := launcher.VerifyKeyViaProxy(proxyPort, secret)
	if !ok {
		return fmt.Errorf("RM-05: %s", hint)
	}
	fmt.Printf("  %s\n", hint)

	fmt.Println("▸ RM-06 profile switch bad key rejected")
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer badSrv.Close()
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[]}`))
	}))
	defer goodSrv.Close()

	pGood := sciconfig.Profile{ID: "pg", Name: "Good", TemplateID: "custom", APIKey: key, BaseURL: goodSrv.URL}
	pBad := sciconfig.Profile{ID: "pb", Name: "Bad", TemplateID: "custom", APIKey: "sk-invalid-key-00000000", BaseURL: badSrv.URL}
	_, err = sciconfig.Update(sciDir, func(c *sciconfig.File) {
		c.Profiles = []sciconfig.Profile{pGood, pBad}
		c.ActiveProfileID = "pg"
	})
	if err != nil {
		return err
	}
	mgr.Reload()
	if _, err := mgr.SwitchProfile("pb"); err == nil {
		return fmt.Errorf("RM-06: expected rejection")
	}
	loaded, _ := sciconfig.Load(sciDir)
	if loaded.ActiveProfileID != "pg" {
		return fmt.Errorf("RM-06: active changed to %q", loaded.ActiveProfileID)
	}
	fmt.Println("  rejected, active unchanged")

	fmt.Println("▸ RM-07 profile switch good key")
	_, err = sciconfig.Update(sciDir, func(c *sciconfig.File) {
		for i := range c.Profiles {
			if c.Profiles[i].ID == "pg" {
				c.Profiles[i].BaseURL = goodSrv.URL
			}
		}
	})
	if err != nil {
		return err
	}
	mgr.Reload()
	res, err := mgr.SwitchProfile("pg")
	if err != nil {
		return fmt.Errorf("RM-07: %w", err)
	}
	fmt.Printf("  action=%s %s\n", res.Action, res.Message)

	fmt.Println("▸ RM-10 CONNECT fast-fail")
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("CONNECT claude.ai:443 HTTP/1.1\r\nHost: claude.ai:443\r\n\r\n"))
	buf := make([]byte, 512)
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), "401") {
		return fmt.Errorf("RM-10: got %q", buf[:n])
	}
	fmt.Println("  401 fast-fail OK")

	fmt.Println("▸ RM-11 quit semantics")
	sbxBefore := launcher.HTTPHealth(sandboxPort, 400)
	mgr.StopProxy()
	time.Sleep(300 * time.Millisecond)
	if !launcher.HTTPHealth(sandboxPort, 400) {
		return fmt.Errorf("RM-11: sandbox stopped after proxy quit")
	}
	if !sbxBefore {
		return fmt.Errorf("RM-11: sandbox was not running before quit")
	}
	fmt.Println("  proxy stopped, sandbox kept")

	fmt.Println("▸ RM-08 relay model discovery")
	if err := runRelayPickerGUI(sciDir, lumenCfg, guiPort); err != nil {
		return fmt.Errorf("RM-08: %w", err)
	}

	fmt.Println("▸ RM-12 official mode switch")
	if _, err := sciconfig.Update(sciDir, func(c *sciconfig.File) {
		c.Mode = "official"
		c.Secret = ""
	}); err != nil {
		return fmt.Errorf("RM-12: %w", err)
	}
	if os.Getenv("SCIENCE_RM_SKIP_OPEN") != "1" {
		if err := launcher.OpenOfficial(); err != nil {
			return fmt.Errorf("RM-12 open official: %w", err)
		}
	}
	cfg2, _ := sciconfig.Load(sciDir)
	if cfg2.Mode != "official" {
		return fmt.Errorf("RM-12: mode=%q", cfg2.Mode)
	}
	fmt.Println("  mode=official, proxy torn down")

	after := pidOnPort(8765)
	if baseline8765 != after {
		return fmt.Errorf("RM-14: 8765 PID changed %s -> %s", baseline8765, after)
	}
	fmt.Printf("▸ RM-14 8765 unchanged (%s)\n", after)

	stopSandboxWithTimeout(sbx, dataDir, guard.ScienceBin(), 8*time.Second)
	return nil
}

func stopSandboxWithTimeout(sbx, dataDir, bin string, timeout time.Duration) {
	done := make(chan error, 1)
	go func() { done <- launcher.Stop(sbx, dataDir, bin) }()
	select {
	case <-done:
	case <-time.After(timeout):
		fmt.Println("  sandbox stop timed out (left for OS cleanup)")
	}
}

func startSandboxWithRetry(mgr *runtime.Manager, attempts int) (string, string, error) {
	var last error
	for i := 0; i < attempts; i++ {
		url, action, err := mgr.StartSandbox()
		if err == nil {
			return url, action, nil
		}
		last = err
		msg := err.Error()
		if strings.Contains(msg, "not our sandbox") || strings.Contains(msg, "探活超时") {
			time.Sleep(8 * time.Second)
			continue
		}
		return "", "", err
	}
	return "", "", last
}

func runRelayPickerGUI(sciDir string, lumenCfg *config.File, _ int) error {
	panel, err := gui.New(gui.Config{
		SciDir: sciDir, LumenCfg: lumenCfg,
		Addr: "127.0.0.1:0", Version: "rm-auto",
	})
	if err != nil {
		return err
	}
	ts := httptest.NewServer(panel.Handler())
	defer ts.Close()
	relaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "m1", "display_name": "Model One"}},
		})
	}))
	defer relaySrv.Close()
	body, _ := json.Marshal(map[string]string{"base_url": relaySrv.URL, "api_key": "tok"})
	resp, err := http.Post(
		ts.URL+"/api/relay/models",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("relay API status %d", resp.StatusCode)
	}
	var out struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if len(out.Models) == 0 || out.Models[0].ID != "m1" {
		return fmt.Errorf("unexpected models: %+v", out.Models)
	}
	fmt.Println("  relay models OK")
	return nil
}

func resolveUpstreamKey(lumenCfg *config.File) (key string, mock *httptest.Server, mode string) {
	spec := proxy.BuiltInProviders["deepseek"]
	seen := map[string]bool{}
	var candidates []string
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		candidates = append(candidates, k)
	}
	add(os.Getenv("DEEPSEEK_API_KEY"))
	add(proxy.KeyFromLumenConfig(lumenCfg, "deepseek"))
	if k, err := proxy.ResolveAPIKey("deepseek", ""); err == nil {
		add(k)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	for _, k := range candidates {
		code, _, err := proxy.ProbeUpstreamKey(ctx, spec, k, "")
		if err == nil && code == 200 {
			return k, nil, "live-deepseek"
		}
	}
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"rm","type":"message","role":"assistant","content":[{"type":"text","text":"pong"}],"model":"deepseek-v4-pro","stop_reason":"end_turn"}`))
	}))
	return "rm-mock-key", mock, "mock-upstream (no valid DeepSeek key)"
}

func pidOnPort(port int) string {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+fmt.Sprint(port), "-sTCP:LISTEN").Output()
	if err != nil {
		return "none"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "none"
	}
	fields := strings.Fields(lines[1])
	if len(fields) >= 2 {
		return fields[1]
	}
	return "none"
}