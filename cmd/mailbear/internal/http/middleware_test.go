package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func reqWith(remoteAddr, xff string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	return req
}

func TestRealIPNoXFF(t *testing.T) {
	require.Equal(t, "203.0.113.5", realIP(reqWith("203.0.113.5:443", "")))
}

func TestRealIPUntrustedDirectIgnoresXFF(t *testing.T) {
	// The direct connection is a public IP, so X-Forwarded-For is untrusted and
	// a spoofed value is ignored in favour of the real connection IP.
	require.Equal(t, "203.0.113.5", realIP(reqWith("203.0.113.5:443", "1.2.3.4")))
}

func TestRealIPTrustedProxyUsesXFF(t *testing.T) {
	// The direct connection is a private address (a local reverse proxy), so the
	// header is trusted and the client IP is taken from it.
	require.Equal(t, "203.0.113.9", realIP(reqWith("10.0.0.1:1234", "203.0.113.9")))
}

func TestRealIPTrustedProxyChainReturnsFirstUntrusted(t *testing.T) {
	got := realIP(reqWith("10.0.0.1:1234", "203.0.113.9, 10.0.0.2, 10.0.0.3"))
	require.Equal(t, "203.0.113.9", got)
}

func TestRealIPAllTrustedReturnsLeftmost(t *testing.T) {
	got := realIP(reqWith("127.0.0.1:1234", "10.0.0.9, 10.0.0.2"))
	require.Equal(t, "10.0.0.9", got)
}

func TestRealIPUnparseableChainFallsBackToDirect(t *testing.T) {
	require.Equal(t, "10.0.0.1", realIP(reqWith("10.0.0.1:1234", "not-an-ip")))
}
