package lab

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"lumen/internal/config"
	labruntime "lumen/internal/science/lab/runtime"
)

const DefaultPort = 18992

// Config for the science lab workbench.
type Config struct {
	SciDir    string
	LumenCfg  *config.File
	Addr      string
	Version   string
	OpenPanel bool
}

// Server hosts Page B — the Lumen Science laboratory.
type Server struct {
	cfg   Config
	api   *API
	fleet *labruntime.FleetManager
	mux   *http.ServeMux
	srv   *http.Server
}

// New builds the lab server.
func New(cfg Config) (*Server, error) {
	if cfg.SciDir == "" {
		return nil, fmt.Errorf("lab: sciDir required")
	}
	if cfg.Addr == "" {
		cfg.Addr = fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	}
	fleet, err := labruntime.NewFleetManager(cfg.SciDir)
	if err != nil {
		return nil, err
	}
	_ = fleet.ConnectDomains("pubmed", "literature", "chembl")
	s := &Server{cfg: cfg, fleet: fleet, mux: http.NewServeMux()}
	s.api = NewAPI(cfg.SciDir, cfg.Version, fleet, parseListenPort(cfg.Addr))
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

// Handler returns the HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.cors(wrapMiddleware(s.mux)))
}

// URL returns the lab URL.
func (s *Server) URL() string {
	return "http://" + s.cfg.Addr
}

// ListenAndServe blocks until shutdown.
func (s *Server) ListenAndServe() error {
	if s.cfg.OpenPanel {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = openPanel(s.URL())
		}()
	}
	handler := s.Handler()
	s.srv = &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	fmt.Printf("Lumen Science 实验室: %s\n", s.URL())
	fmt.Println("Ctrl+C 退出实验室")
	return s.srv.Serve(ln)
}

// Shutdown stops the server and fleet.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.fleet != nil {
		s.fleet.Close()
	}
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
			origin == "http://localhost:3100"
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

func parseListenPort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return DefaultPort
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return DefaultPort
	}
	return p
}

func openPanel(url string) error {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("open", "-na", "Google Chrome", "--args", "--app="+url).Run(); err == nil {
			return nil
		}
		return exec.Command("open", url).Run()
	}
	return exec.Command("xdg-open", url).Run()
}
