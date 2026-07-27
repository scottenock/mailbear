// Package http is the transport layer: it wires a chi router (with CORS, rate
// limiting, request logging, a body-size cap and a client-IP extractor) to the
// domain.Mailer, and exposes Prometheus metrics on a separate listener.
package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

const (
	rateLimitWindow = time.Minute
	maxBodyBytes    = 64 << 10 // 64 KiB — form submissions are tiny.

	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// Server is the HTTP transport layer.
type Server struct {
	api     *http.Server
	metrics *http.Server
	logger  zerolog.Logger
	mailer  domain.Mailer
	version string
}

// New builds the API and metrics servers.
func New(logger zerolog.Logger, mailer domain.Mailer, httpAddress, metricsAddr string, rateLimit int, version string) *Server {
	s := &Server{
		logger:  logger.With().Str("layer", "http").Logger(),
		mailer:  mailer,
		version: version,
	}

	router := chi.NewRouter()
	router.Use(maxBytesMiddleware(maxBodyBytes))
	router.Use(requestLoggerMiddleware(s.logger))
	router.Use(httprate.LimitBy(
		rateLimit,
		rateLimitWindow,
		func(r *http.Request) (string, error) { return realIP(r), nil },
		httprate.WithLimitHandler(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusTooManyRequests, "rate limit exceeded")
		}),
	))
	router.Use(cors.Handler(cors.Options{AllowedOrigins: []string{"*"}}))

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, "Welcome to mail bear! 🐻")
		})
		r.Post("/form/{id}", s.handleForm)
	})

	s.api = newHTTPServer(httpAddress, router)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	s.metrics = newHTTPServer(metricsAddr, metricsMux)

	return s
}

// newHTTPServer builds an http.Server with hardened timeouts (guarding against
// slow-client/slowloris connections holding resources open indefinitely).
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Serve starts the metrics server in the background and the API server in the
// foreground. It blocks until the API server is shut down.
func (s *Server) Serve() error {
	go func() {
		if err := s.metrics.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error().Err(err).Msg("metrics server stopped unexpectedly")
		}
	}()

	s.logger.Info().Str("version", s.version).Str("address", s.api.Addr).Msg("Starting MailBear")

	if err := s.api.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// Shutdown gracefully drains the API and metrics servers.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.api.Shutdown(ctx); err != nil {
		return err
	}
	return s.metrics.Shutdown(ctx)
}
