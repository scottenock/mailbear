// Package http is the transport layer: it wires an echo server (with CORS, rate
// limiting, request logging and a client-IP extractor) to the domain.Mailer, and
// exposes Prometheus metrics on a separate listener.
package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// rateLimitPeriod is the window the per-IP rate limit is measured over.
const rateLimitPeriod = time.Minute

// Server is the HTTP transport layer.
type Server struct {
	api         *echo.Echo
	metrics     *echo.Echo
	httpAddress string
	metricsAddr string
	logger      zerolog.Logger
	mailer      domain.Mailer
	version     string
}

// New builds the API and metrics servers.
func New(logger zerolog.Logger, mailer domain.Mailer, httpAddress, metricsAddr string, rateLimit int, version string) *Server {
	s := &Server{
		httpAddress: httpAddress,
		metricsAddr: metricsAddr,
		logger:      logger.With().Str("layer", "http").Logger(),
		mailer:      mailer,
		version:     version,
	}

	api := echo.New()
	api.HideBanner = true
	api.HidePort = true

	// Trust X-Forwarded-For only when the immediate connection comes from a
	// loopback/link-local/private-net address (i.e. a reverse proxy on the same
	// host or private network), walking back to the real client IP. This keeps
	// the rate limiter working behind a local reverse proxy while discarding
	// values a client tries to spoof further up the chain.
	api.IPExtractor = echo.ExtractIPFromXFFHeader()

	api.Use(requestLoggerMiddleware(s.logger))
	api.Use(rateLimitMiddleware(rateLimitPeriod, int64(rateLimit)))
	api.Use(echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins: []string{"*"},
	}))

	group := api.Group("/api/v1")
	group.GET("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, response("Welcome to mail bear! 🐻"))
	})
	group.POST("/form/:id", s.handleForm)

	s.api = api

	metrics := echo.New()
	metrics.HideBanner = true
	metrics.HidePort = true
	metrics.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	s.metrics = metrics

	return s
}

// Serve starts the metrics server in the background and the API server in the
// foreground. It blocks until the API server is shut down.
func (s *Server) Serve() error {
	go func() {
		if err := s.metrics.Start(s.metricsAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error().Err(err).Msg("metrics server stopped unexpectedly")
		}
	}()

	s.logger.Info().Str("version", s.version).Str("address", s.httpAddress).Msg("Starting MailBear")

	if err := s.api.Start(s.httpAddress); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
