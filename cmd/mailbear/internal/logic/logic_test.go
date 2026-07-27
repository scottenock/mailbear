package logic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T, opts ...Option) *Service {
	t.Helper()

	base := []Option{
		WithSettings(domain.Settings{
			SMTP: domain.SMTP{Host: "localhost", Port: 25, FromEmail: "no-reply@example.com"},
		}),
		WithForms([]*domain.Form{{Key: "some-key", HumanReadableName: "some-form"}}),
	}

	svc, err := New(zerolog.Nop(), append(base, opts...)...)
	require.NoError(t, err)
	return svc
}

func TestNewRequiresSettings(t *testing.T) {
	_, err := New(zerolog.Nop(), WithForms([]*domain.Form{{Key: "k"}}))
	require.Error(t, err, "should require SMTP settings")

	_, err = New(zerolog.Nop(), WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}))
	require.Error(t, err, "should require at least one form")
}

func TestFormByKey(t *testing.T) {
	svc := newTestService(t)

	require.NotNil(t, svc.FormByKey("some-key"), "existing form should be found")
	require.Nil(t, svc.FormByKey("missing-key"), "unknown form should return nil")
}

func TestTurnstileEnabled(t *testing.T) {
	require.False(t, newTestService(t).TurnstileEnabled(), "disabled when no secret")

	svc := newTestService(t, WithSettings(domain.Settings{
		SMTP:            domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"},
		TurnstileSecret: "secret",
	}))
	require.True(t, svc.TurnstileEnabled(), "enabled when secret set")
}

func TestVerifyTurnstile(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer srv.Close()

	orig := turnstileVerifyURL
	turnstileVerifyURL = srv.URL
	defer func() { turnstileVerifyURL = orig }()

	svc := newTestService(t, WithSettings(domain.Settings{
		SMTP:            domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"},
		TurnstileSecret: "the-secret",
	}))

	ok, err := svc.VerifyTurnstile(context.Background(), "token", "1.2.3.4")
	require.NoError(t, err)
	require.True(t, ok, "valid token should pass")
	require.Contains(t, body, "secret=the-secret")
	require.Contains(t, body, "remoteip=1.2.3.4")

	// An empty token is a failed challenge, not an error, and makes no HTTP call.
	ok, err = svc.VerifyTurnstile(context.Background(), "", "")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestVerifyTurnstileRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success": false, "error-codes": ["invalid-input-response"]}`))
	}))
	defer srv.Close()

	orig := turnstileVerifyURL
	turnstileVerifyURL = srv.URL
	defer func() { turnstileVerifyURL = orig }()

	svc := newTestService(t, WithSettings(domain.Settings{
		SMTP:            domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"},
		TurnstileSecret: "the-secret",
	}))

	ok, err := svc.VerifyTurnstile(context.Background(), "bad-token", "")
	require.NoError(t, err)
	require.False(t, ok, "rejected token should fail")
}

func TestSubjectTemplateRender(t *testing.T) {
	svc, err := New(
		zerolog.Nop(),
		WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}),
		WithForms([]*domain.Form{{Key: "k", HumanReadableName: "contact", Subject: "New {{.FormName}} from {{.Name}}"}}),
	)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, svc.mailers["k"].subject.Execute(&buf, domain.TemplateData{Name: "Joe", FormName: "contact"}))
	require.Equal(t, "New contact from Joe", buf.String())
}

func TestNewUnknownTemplate(t *testing.T) {
	_, err := New(
		zerolog.Nop(),
		WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}),
		WithForms([]*domain.Form{{Key: "k", Template: "does-not-exist"}}),
	)
	require.Error(t, err, "unknown template should fail at startup")
}

func TestNewBadSubjectTemplate(t *testing.T) {
	_, err := New(
		zerolog.Nop(),
		WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}),
		WithForms([]*domain.Form{{Key: "k", Subject: "{{ .Name "}}),
	)
	require.Error(t, err, "malformed subject template should fail at startup")
}
