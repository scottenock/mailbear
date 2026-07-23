// Package logic contains the business logic: form lookup, mail composition and
// delivery, and Turnstile verification. Service implements domain.Mailer.
package logic

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	mail "gopkg.in/mail.v2"
)

// Ensure Service implements the interface.
var _ domain.Mailer = (*Service)(nil)

var formSubmissionsCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mailbear_form_submissions_total",
		Help: "How many form submissions handled, partitioned by form name.",
	},
	[]string{"form"},
)

// Service is the concrete implementation of domain.Mailer.
type Service struct {
	logger     zerolog.Logger
	settings   domain.Settings
	forms      []*domain.Form
	httpClient *http.Client
}

// Option configures a Service.
type Option func(*Service)

// WithSettings sets the SMTP and Turnstile settings.
func WithSettings(settings domain.Settings) Option {
	return func(s *Service) { s.settings = settings }
}

// WithForms sets the configured forms.
func WithForms(forms []*domain.Form) Option {
	return func(s *Service) { s.forms = forms }
}

// WithHTTPClient overrides the HTTP client used for Turnstile verification.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) { s.httpClient = client }
}

// New builds a Service and validates that the required settings are present.
func New(logger zerolog.Logger, opts ...Option) (*Service, error) {
	s := &Service{
		logger:     logger.With().Str("layer", "logic").Logger(),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.settings.SMTP.Host == "" {
		return nil, fmt.Errorf("smtp host must be set")
	}
	if s.settings.SMTP.Port == 0 {
		return nil, fmt.Errorf("smtp port must be set")
	}
	if s.settings.SMTP.FromEmail == "" {
		return nil, fmt.Errorf("smtp from email must be set")
	}
	if len(s.forms) == 0 {
		return nil, fmt.Errorf("expected to have at least one form")
	}

	return s, nil
}

// FormByKey returns the form with the given key, or nil if none exists.
func (s *Service) FormByKey(key string) *domain.Form {
	for _, form := range s.forms {
		if form.Key == key {
			return form
		}
	}
	return nil
}

// TurnstileEnabled reports whether Turnstile verification is configured.
func (s *Service) TurnstileEnabled() bool {
	return s.settings.TurnstileSecret != ""
}

// Send delivers a form submission to the form's recipients.
func (s *Service) Send(_ context.Context, submission *domain.FormSubmission) error {
	form := s.FormByKey(submission.FormID)
	if form == nil {
		return fmt.Errorf("form does not exist")
	}

	msg := mail.NewMessage()
	msg.SetHeader("From", s.settings.SMTP.FromEmail)
	msg.SetHeader("To", form.ToEmail...)
	msg.SetAddressHeader("Reply-To", submission.Email, submission.Name)
	msg.SetHeader("Subject", fmt.Sprintf("New submission with subject: %s", submission.Subject))
	msg.SetBody("text/html", buildMailBody(submission))

	dialer := mail.NewDialer(
		s.settings.SMTP.Host,
		s.settings.SMTP.Port,
		s.settings.SMTP.User,
		s.settings.SMTP.Password,
	)
	if s.settings.SMTP.DisableTLS {
		dialer.StartTLSPolicy = mail.NoStartTLS
	}

	if err := dialer.DialAndSend(msg); err != nil {
		return errors.Wrap(err, "couldn't send the email")
	}

	formSubmissionsCounter.With(prometheus.Labels{"form": form.HumanReadableName}).Add(1)

	return nil
}

// buildMailBody renders the HTML email body, escaping all user-supplied fields to
// prevent HTML/link injection into the recipient's inbox.
func buildMailBody(submission *domain.FormSubmission) string {
	const template = `
	<p>Hello,</p>
	<p>Someone has just submitted a new form on your website.</p>
	<p>Kind regards,<br>MailBear</p>
	<p><br></p>
	<p><b>Name:</b> %s</p>
	<p><b>Email:</b> %s</p>
	<p><b>Subject:</b> %s</p>
	<p><b>Content:</b><br><br>%s</p>
	`

	content := strings.ReplaceAll(html.EscapeString(submission.Content), "\n", "<br>")

	return fmt.Sprintf(
		template,
		html.EscapeString(submission.Name),
		html.EscapeString(submission.Email),
		html.EscapeString(submission.Subject),
		content,
	)
}
