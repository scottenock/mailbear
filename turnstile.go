package mailbear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// turnstileVerifyURL is Cloudflare's siteverify endpoint. It is a var (not a const)
// so tests can point it at a local httptest server.
var turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// turnstileResponse is the relevant subset of Cloudflare's siteverify response.
type turnstileResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// verifyTurnstile validates a Turnstile response token against Cloudflare's
// siteverify endpoint. remoteIP is optional and, when set, is included so
// Cloudflare can cross-check the token against the client's address.
func (m *MailBear) verifyTurnstile(ctx context.Context, token, remoteIP string) (bool, error) {
	if token == "" {
		// No token supplied — treat as a failed challenge rather than an error.
		return false, nil
	}

	form := url.Values{}
	form.Set("secret", m.config.Global.Turnstile.Secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, errors.Wrap(err, "couldn't build turnstile request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false, errors.Wrap(err, "couldn't reach turnstile siteverify")
	}
	defer func() { _ = resp.Body.Close() }()

	var result turnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, errors.Wrap(err, "couldn't decode turnstile response")
	}

	return result.Success, nil
}
