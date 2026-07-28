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
	_, err := New(zerolog.Nop(), WithForms([]*domain.Form{{Key: "k", ToEmail: []string{"a@b.com"}}}))
	require.Error(t, err, "should require SMTP settings when a form sends email")

	_, err = New(zerolog.Nop(), WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}))
	require.Error(t, err, "should require at least one form")
}

func TestNewWebhookOnlyNeedsNoSMTP(t *testing.T) {
	_, err := New(
		zerolog.Nop(),
		WithForms([]*domain.Form{{Key: "k", WebhookURL: "https://example.com/hook"}}),
	)
	require.NoError(t, err, "a webhook-only form should not require SMTP settings")
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

func TestSendWebhookOnly(t *testing.T) {
	var gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc, err := New(
		zerolog.Nop(),
		WithForms([]*domain.Form{{Key: "hook-key", HumanReadableName: "alerts", WebhookURL: srv.URL}}),
	)
	require.NoError(t, err)

	err = svc.Send(context.Background(), &domain.FormSubmission{
		FormID:  "hook-key",
		Name:    "Ada",
		Email:   "ada@example.com",
		Subject: "Hi",
		Content: "hello",
	})
	require.NoError(t, err)
	require.Equal(t, "application/json", gotContentType)
	require.JSONEq(t, `{"form":"alerts","name":"Ada","email":"ada@example.com","subject":"Hi","content":"hello"}`, gotBody)
}

func TestSendWebhookOnlyFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc, err := New(
		zerolog.Nop(),
		WithForms([]*domain.Form{{Key: "hook-key", HumanReadableName: "alerts", WebhookURL: srv.URL}}),
	)
	require.NoError(t, err)

	err = svc.Send(context.Background(), &domain.FormSubmission{FormID: "hook-key", Email: "a@b.com", Subject: "s", Content: "c"})
	require.Error(t, err, "a webhook-only form must fail when the webhook rejects the request")
}

func TestNewAutoresponder(t *testing.T) {
	svc, err := New(
		zerolog.Nop(),
		WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}),
		WithForms([]*domain.Form{{
			Key:               "k",
			HumanReadableName: "contact",
			ToEmail:           []string{"owner@example.com"},
			Autoresponder:     &domain.Autoresponder{Template: "default", Subject: "Thanks {{.Name}}"},
		}}),
	)
	require.NoError(t, err)

	ar := svc.mailers["k"].autoresponder
	require.NotNil(t, ar, "autoresponder should be built")

	var buf strings.Builder
	require.NoError(t, ar.subject.Execute(&buf, domain.TemplateData{Name: "Ada"}))
	require.Equal(t, "Thanks Ada", buf.String())
}

func TestNewAutoresponderUnknownTemplate(t *testing.T) {
	_, err := New(
		zerolog.Nop(),
		WithSettings(domain.Settings{SMTP: domain.SMTP{Host: "h", Port: 25, FromEmail: "f@x"}}),
		WithForms([]*domain.Form{{
			Key:           "k",
			ToEmail:       []string{"owner@example.com"},
			Autoresponder: &domain.Autoresponder{Template: "does-not-exist"},
		}}),
	)
	require.Error(t, err, "autoresponder referencing a missing template should fail at startup")
}

// memStore is an in-memory domain.Store for tests.
type memStore struct {
	records []domain.SubmissionRecord
}

func (m *memStore) Save(record domain.SubmissionRecord) error {
	m.records = append(m.records, record)
	return nil
}

func (m *memStore) Close() error { return nil }

func TestSendPersistsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st := &memStore{}
	svc, err := New(
		zerolog.Nop(),
		WithForms([]*domain.Form{{Key: "k", HumanReadableName: "contact", WebhookURL: srv.URL}}),
		WithStore(st),
	)
	require.NoError(t, err)

	require.NoError(t, svc.Send(context.Background(), &domain.FormSubmission{FormID: "k", Email: "a@b.com", Subject: "s", Content: "c"}))
	require.Len(t, st.records, 1)
	require.Equal(t, "contact", st.records[0].Form)
	require.Equal(t, "a@b.com", st.records[0].Email)
	require.True(t, st.records[0].Delivered, "successful delivery records delivered=true")
}

func TestSendPersistsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	st := &memStore{}
	svc, err := New(
		zerolog.Nop(),
		WithForms([]*domain.Form{{Key: "k", HumanReadableName: "contact", WebhookURL: srv.URL}}),
		WithStore(st),
	)
	require.NoError(t, err)

	err = svc.Send(context.Background(), &domain.FormSubmission{FormID: "k", Email: "a@b.com", Subject: "s", Content: "c"})
	require.Error(t, err, "webhook-only delivery failure returns an error")
	require.Len(t, st.records, 1, "a failed delivery is still recorded")
	require.False(t, st.records[0].Delivered, "failed delivery records delivered=false")
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
