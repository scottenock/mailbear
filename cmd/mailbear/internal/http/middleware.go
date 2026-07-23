package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

// rateLimitMiddleware limits requests per client IP over the given period.
// Adapted from https://gitter.im/labstack/echo?at=5a90e681a2194eb80da6faff
func rateLimitMiddleware(period time.Duration, limit int64) echo.MiddlewareFunc {
	rate := limiter.Rate{Period: period, Limit: limit}
	instance := limiter.New(memory.NewStore(), rate)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			context, err := instance.Get(c.Request().Context(), c.RealIP())
			if err != nil {
				// On limiter backend errors, fail open and let the request through.
				return next(c)
			}

			h := c.Response().Header()
			h.Add("X-RateLimit-Limit", strconv.FormatInt(context.Limit, 10))
			h.Add("X-RateLimit-Remaining", strconv.FormatInt(context.Remaining, 10))
			h.Add("X-RateLimit-Reset", strconv.FormatInt(context.Reset, 10))

			if context.Reached {
				return c.JSON(http.StatusTooManyRequests, response("rate limit exceeded"))
			}

			return next(c)
		}
	}
}

// requestLoggerMiddleware logs each request via zerolog.
func requestLoggerMiddleware(logger zerolog.Logger) echo.MiddlewareFunc {
	return echomw.RequestLoggerWithConfig(echomw.RequestLoggerConfig{
		LogStatus:   true,
		LogMethod:   true,
		LogURI:      true,
		LogRemoteIP: true,
		LogLatency:  true,
		LogError:    true,
		LogValuesFunc: func(_ echo.Context, v echomw.RequestLoggerValues) error {
			evt := logger.Info()
			if v.Error != nil {
				evt = logger.Error().Err(v.Error)
			}
			evt.Str("method", v.Method).
				Str("uri", v.URI).
				Int("status", v.Status).
				Str("remote_ip", v.RemoteIP).
				Dur("latency", v.Latency).
				Msg("request")
			return nil
		},
	})
}
