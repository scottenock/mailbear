package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const validJSON = `{"name":"Ada","email":"ada@example.com","subject":"Hi","content":"hello"}`

// fakeMailer is a configurable domain.Mailer stub for exercising the handler
// without SMTP or network access.
type fakeMailer struct {
	form         *domain.Form
	turnstileOn  bool
	turnstileOK  bool
	turnstileErr error
	sendErr      error

	sent         []*domain.FormSubmission
	verifyCalls  int
	lastVerifyIP string
}

func (f *fakeMailer) FormByKey(key string) *domain.Form {
	if f.form != nil && f.form.Key == key {
		return f.form
	}
	return nil
}

func (f *fakeMailer) TurnstileEnabled() bool { return f.turnstileOn }

func (f *fakeMailer) VerifyTurnstile(_ context.Context, _, remoteIP string) (bool, error) {
	f.verifyCalls++
	f.lastVerifyIP = remoteIP
	return f.turnstileOK, f.turnstileErr
}

func (f *fakeMailer) Send(_ context.Context, sub *domain.FormSubmission) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sub)
	return nil
}

func testForm() *domain.Form {
	return &domain.Form{
		Key:               "contact-key",
		HumanReadableName: "contact",
		AllowedDomains:    []string{"example.com"},
		ToEmail:           []string{"to@example.com"},
	}
}

func newTestServer(m domain.Mailer, rateLimit int) *Server {
	return New(zerolog.Nop(), m, ":0", ":0", rateLimit, "test")
}

func do(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.api.Handler.ServeHTTP(rec, req)
	return rec
}

// jsonPost builds a JSON POST to the contact form with an allowed Origin.
func jsonPost(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/form/contact-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	return req
}

// formPost builds a browser-style (form-urlencoded) POST with an allowed Origin.
func formPost(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/form/contact-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	return req
}

const validForm = "email=ada@example.com&subject=Hi&content=hello"

func TestWelcome(t *testing.T) {
	rec := do(newTestServer(&fakeMailer{form: testForm()}, 1000), httptest.NewRequest(http.MethodGet, "/api/v1/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "mail bear")
}

func TestHealthEndpoints(t *testing.T) {
	s := newTestServer(&fakeMailer{form: testForm()}, 1000)
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := do(s, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
	}
}

func TestHealthNotRateLimited(t *testing.T) {
	s := newTestServer(&fakeMailer{form: testForm()}, 1) // 1 req/min would trip normal routes
	for i := range 5 {
		rec := do(s, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		require.Equal(t, http.StatusOK, rec.Code, "probe %d should not be rate limited", i)
	}
}

func TestHandleFormUnknownForm(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/form/nope", strings.NewReader(validJSON))
	req.Header.Set("Content-Type", "application/json")

	rec := do(newTestServer(m, 1000), req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormValidJSON(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	rec := do(newTestServer(m, 1000), jsonPost(validJSON))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, m.sent, 1)
	require.Equal(t, "ada@example.com", m.sent[0].Email)
	require.Equal(t, "contact-key", m.sent[0].FormID)
}

func TestHandleFormValidFormURLEncoded(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/form/contact-key",
		strings.NewReader("name=Ada&email=ada@example.com&subject=Hi&content=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")

	rec := do(newTestServer(m, 1000), req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, m.sent, 1)
	require.Equal(t, "hello", m.sent[0].Content)
}

func TestHandleFormInvalidJSON(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	rec := do(newTestServer(m, 1000), jsonPost(`{not json`))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormValidationFailure(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	rec := do(newTestServer(m, 1000), jsonPost(`{"email":"a@b.com","subject":"s"}`)) // no content

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormHoneypot(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	rec := do(newTestServer(m, 1000), jsonPost(`{"email":"a@b.com","subject":"s","content":"c","_gotcha":"bot"}`))

	// Silent success, but the submission is dropped (never sent).
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormDisallowedOrigin(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	req := jsonPost(validJSON)
	req.Header.Set("Origin", "http://evil.com")

	rec := do(newTestServer(m, 1000), req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormTurnstileInvalid(t *testing.T) {
	m := &fakeMailer{form: testForm(), turnstileOn: true, turnstileOK: false}
	rec := do(newTestServer(m, 1000), jsonPost(validJSON))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, 1, m.verifyCalls)
	require.Empty(t, m.sent)
}

func TestHandleFormTurnstileError(t *testing.T) {
	m := &fakeMailer{form: testForm(), turnstileOn: true, turnstileErr: errors.New("upstream down")}
	rec := do(newTestServer(m, 1000), jsonPost(validJSON))

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Empty(t, m.sent)
}

func TestHandleFormTurnstileOKPassesRealIP(t *testing.T) {
	m := &fakeMailer{form: testForm(), turnstileOn: true, turnstileOK: true}
	rec := do(newTestServer(m, 1000), jsonPost(validJSON))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, m.sent, 1)
	require.Equal(t, 1, m.verifyCalls)
	require.Equal(t, "192.0.2.1", m.lastVerifyIP) // httptest's default RemoteAddr
}

func TestHandleFormSendError(t *testing.T) {
	m := &fakeMailer{form: testForm(), sendErr: errors.New("smtp down")}
	rec := do(newTestServer(m, 1000), jsonPost(validJSON))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleFormRateLimit(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	s := newTestServer(m, 1) // 1 request per IP per minute

	require.Equal(t, http.StatusOK, do(s, jsonPost(validJSON)).Code)
	require.Equal(t, http.StatusTooManyRequests, do(s, jsonPost(validJSON)).Code)
}

func TestRedirectOnSuccess(t *testing.T) {
	form := testForm()
	form.RedirectURL = "https://example.com/thanks"
	m := &fakeMailer{form: form}

	rec := do(newTestServer(m, 1000), formPost(validForm))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "https://example.com/thanks", rec.Header().Get("Location"))
	require.Len(t, m.sent, 1)
}

func TestRedirectHoneypotLooksLikeSuccess(t *testing.T) {
	form := testForm()
	form.RedirectURL = "https://example.com/thanks"
	m := &fakeMailer{form: form}

	rec := do(newTestServer(m, 1000), formPost(validForm+"&_gotcha=bot"))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "https://example.com/thanks", rec.Header().Get("Location"))
	require.Empty(t, m.sent, "honeypot submission is dropped but still redirects to success")
}

func TestRedirectOnError(t *testing.T) {
	form := testForm()
	form.ErrorRedirectURL = "https://example.com/oops"
	m := &fakeMailer{form: form}

	rec := do(newTestServer(m, 1000), formPost("email=a@b.com&subject=s")) // missing content
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "https://example.com/oops", rec.Header().Get("Location"))
	require.Empty(t, m.sent)
}

func TestRedirectIgnoredForJSONClients(t *testing.T) {
	form := testForm()
	form.RedirectURL = "https://example.com/thanks"
	m := &fakeMailer{form: form}

	rec := do(newTestServer(m, 1000), jsonPost(validJSON))
	require.Equal(t, http.StatusOK, rec.Code, "AJAX/JSON clients get JSON, never a redirect")
	require.Empty(t, rec.Header().Get("Location"))
}

func TestNoRedirectFallsBackToJSON(t *testing.T) {
	m := &fakeMailer{form: testForm()} // no redirect_url configured
	rec := do(newTestServer(m, 1000), formPost(validForm))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Location"))
}

func TestHandleFormBodyLimit(t *testing.T) {
	m := &fakeMailer{form: testForm()}
	big := strings.Repeat("a", 70*1024) // exceeds the 64 KiB cap
	body := `{"email":"a@b.com","subject":"s","content":"` + big + `"}`

	rec := do(newTestServer(m, 1000), jsonPost(body))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, m.sent)
}
