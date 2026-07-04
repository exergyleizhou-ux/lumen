package gui

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"lumen/internal/config"
)

const DefaultPort = 18990

// Config for the science control panel.
type Config struct {
	SciDir    string
	LumenCfg  *config.File
	Addr      string // default 127.0.0.1:18990
	Version   string
	OpenPanel bool
}

// Server hosts the Grok Build-style science control panel.
type Server struct {
	cfg       Config
	api       *API
	mux       *http.ServeMux
	srv       *http.Server
	startedAt time.Time
}

// New builds the GUI server.
func New(cfg Config) (*Server, error) {
	if cfg.SciDir == "" {
		return nil, fmt.Errorf("gui: sciDir required")
	}
	if cfg.Addr == "" {
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	}
	startedAt := time.Now()
	api := NewAPI(cfg.SciDir, cfg.LumenCfg, cfg.Version, startedAt)
	s := &Server{cfg: cfg, api: api, mux: http.NewServeMux(), startedAt: startedAt}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	static, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(static))
	assetHandler := http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		fileServer.ServeHTTP(w, r)
	}))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "panel missing", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
	s.mux.Handle("/assets/", assetHandler)
	s.api.Register(s.mux)
}

// URL returns the panel URL.
func (s *Server) URL() string {
	return "http://" + s.cfg.Addr
}

// Handler returns the middleware-wrapped mux (for RM automation / embed tests).
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.cors(rateLimitMutations(s.wrapMiddleware(s.mux))))
}

// ListenAndServe blocks until shutdown.
func (s *Server) ListenAndServe() error {
	if s.cfg.OpenPanel {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = openPanel(s.URL())
		}()
	}
	handler := securityHeaders(s.cors(rateLimitMutations(s.wrapMiddleware(s.mux))))
	s.srv = &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           handler,
		MaxHeaderBytes:    1 << 16,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		ln, err := net.Listen("tcp", s.cfg.Addr)
		if err != nil {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}
		fmt.Printf("Lumen Science 控制面板: %s\n", s.URL())
		fmt.Println("Ctrl+C 退出面板（沙箱保持运行，代理继续由 science 管理）")
		return s.srv.Serve(ln)
	}
	return fmt.Errorf("science gui: cannot bind %s after 10 attempts: %w", s.cfg.Addr, lastErr)
}

// Shutdown stops the HTTP server and panel proxy.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.api != nil {
		s.api.StopProxyOnly()
	}
	resetPanelManager()
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin == "http://"+s.cfg.Addr ||
			origin == "https://demo.oasisdata2026.xyz" ||
			origin == "http://localhost:3100" ||
			origin == "http://localhost:3200"
		if allowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "http://"+s.cfg.Addr)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func openPanel(url string) error {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("open", "-na", "Google Chrome", "--args", "--app="+url).Run(); err == nil {
			return nil
		}
		if err := exec.Command("open", "-na", "Chromium", "--args", "--app="+url).Run(); err == nil {
			return nil
		}
		return exec.Command("open", url).Run()
	}
	return exec.Command("xdg-open", url).Run()
}

// QuitProxy stops in-process proxy only (panel close semantics).
func (s *Server) QuitProxy() {
	s.api.StopProxyOnly()
}
