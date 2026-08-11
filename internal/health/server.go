package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

type Readiness interface {
	Ready() bool
}

type Server struct {
	srv     *http.Server
	ready   Readiness
	healthy atomic.Bool
}

func New(addr string, ready Readiness, metrics http.Handler, logger *slog.Logger) *Server {
	s := &Server{ready: ready}
	s.healthy.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/readyz", s.readyz)
	if metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return s
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		s.healthy.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	if !s.healthy.Load() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	if s.ready != nil && !s.ready.Ready() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = fmt.Fprintln(w, "ready")
}
