package webhook

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rigdev/rig/internal/config"
)

// Server is the webhook HTTP server.
type Server struct {
	handler *Handler
	cfg     config.ServerConfig
	srv     *http.Server
}

// NewServer creates a new webhook Server.
func NewServer(cfg config.ServerConfig, handler *Handler) *Server {
	return &Server{
		handler: handler,
		cfg:     cfg,
	}
}

// ListenAndServe starts the webhook server with graceful shutdown.
// It blocks until the context is cancelled or a termination signal is received.
func (s *Server) ListenAndServe(ctx context.Context) error {
	r := chi.NewRouter()

	// Body size limit: 10MB
	r.Use(bodySizeLimitMiddleware(10 << 20))

	r.Post("/webhook", s.handler.HandleWebhook)

	port := s.cfg.Port
	if port == 0 {
		port = 8080
	}

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}

	// Set up signal-based graceful shutdown.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("webhook server listening on :%d", port)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down webhook server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// Router returns the chi router for testing purposes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(bodySizeLimitMiddleware(10 << 20))
	r.Post("/webhook", s.handler.HandleWebhook)
	return r
}

// bodySizeLimitMiddleware limits the request body size.
func bodySizeLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
