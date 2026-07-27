package http

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// maxBytesMiddleware caps the request body size to guard against a client sending
// an arbitrarily large body and exhausting server memory.
func maxBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// requestLoggerMiddleware logs each request via zerolog.
func requestLoggerMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				evt := logger.Info()
				if ww.Status() >= http.StatusInternalServerError {
					evt = logger.Error()
				}
				evt.Str("method", r.Method).
					Str("uri", r.RequestURI).
					Int("status", ww.Status()).
					Str("remote_ip", realIP(r)).
					Dur("latency", time.Since(start)).
					Msg("request")
			}()

			next.ServeHTTP(ww, r)
		})
	}
}

// realIP resolves the client IP. It trusts X-Forwarded-For only when the immediate
// connection comes from a loopback/link-local/private-network address (i.e. a
// reverse proxy on the same host or private network), walking the chain back to
// the first untrusted hop. This keeps the rate limiter working behind a local
// reverse proxy while discarding values a client tries to spoof further up.
func realIP(r *http.Request) string {
	directIP := directRemoteIP(r)

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return directIP
	}

	ip := net.ParseIP(directIP)
	if ip == nil || !trustedProxy(ip) {
		return directIP
	}

	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := net.ParseIP(strings.TrimSpace(parts[i]))
		if candidate == nil {
			// Unparseable entry: can't trust the rest of the chain.
			return directIP
		}
		if !trustedProxy(candidate) {
			return candidate.String()
		}
	}

	// All entries are trusted: return the furthest (leftmost) as a best effort.
	return strings.TrimSpace(parts[0])
}

func directRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trustedProxy(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}
