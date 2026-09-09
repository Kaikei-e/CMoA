// Package serve is CMoA's OpenAI-compatible HTTP face. It runs every
// proposer and the judge, then returns the selected answer. Each request
// writes a task directory and complete run trace, so a served answer is as
// reconstructible as one produced by the CLI.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/llm"
)

// Options tune a server.
type Options struct {
	AsOf    string          // YYYY-MM-DD; empty means today
	Version string          // cmoa version string for run.json
	Harness *harnessdir.Dir // nil means no harness directory
	Client  *llm.Client     // nil means a default client
	Log     func(format string, args ...any)
	Now     func() time.Time
}

// Server answers /v1/models and /v1/chat/completions.
type Server struct {
	cfg   *config.Config
	opt   Options
	sem   chan struct{}
	once  sync.Once
	logMu sync.Mutex
}

// New validates that cfg can serve and returns the server.
func New(cfg *config.Config, opt Options) (*Server, error) {
	if cfg.Serve == nil {
		return nil, errors.New("serve: cmoa.json declares no serve block (version 2 adds it)")
	}
	if cfg.Judge == nil {
		return nil, errors.New("serve: cmoa.json declares no judge; the chat face cannot select without one")
	}
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Client == nil {
		opt.Client = &llm.Client{HTTP: &http.Client{}}
	}
	s := &Server{
		cfg: cfg,
		opt: opt,
		sem: make(chan struct{}, cfg.Serve.MaxInflight),
	}
	logf := s.opt.Log
	s.opt.Log = func(format string, args ...any) {
		s.logMu.Lock()
		defer s.logMu.Unlock()
		logf(format, args...)
	}
	return s, nil
}

// Handler is the routing table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("POST /v1/chat/completions", s.completions)
	return mux
}

// CheckListen refuses a non-loopback address unless the caller said so on
// the command line. There is no auth and no TLS here: binding a fleet's
// front door to the network is a decision a person types, not a default.
func CheckListen(addr string, allowRemote bool) error {
	if allowRemote {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("serve: listen %q is not host:port: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("serve: listen %q binds every interface; cmoa serve has no auth, so pass --allow-remote to mean it", addr)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	return fmt.Errorf("serve: listen %q is not a loopback address; cmoa serve has no auth, so pass --allow-remote to mean it", addr)
}

// ListenAndServe serves until ctx is done, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Serve.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", s.cfg.Serve.Listen)
	if err != nil {
		return err
	}
	s.opt.Log("listening on http://%s (pool %q, %d in flight, runs under %s)",
		ln.Addr(), s.cfg.Serve.PoolName, s.cfg.Serve.MaxInflight, s.cfg.Serve.RunsDir)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.opt.Log("shutting down")
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
