package api

import (
	"context"
	"net/http"
	"time"
)

// Server wraps the underlying http.Server to provide graceful shutdown.
type Server struct {
	httpServer *http.Server
}

// NewServer creates an HTTP server listening on addr and serving handler.
func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

// ListenAndServe starts the server and blocks until it exits.
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server using the provided context.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
