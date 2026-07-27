// Package logic contains the business logic: form lookup, mail composition and
// delivery, and Turnstile verification. Service implements domain.Mailer.
package logic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	textTmpl "text/template"
	"time"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	mail "gopkg.in/mail.v2"
)

// defaultSubjectTemplate reproduces the historical subject line when a form does
// not configure its own.
const defaultSubjectTemplate = "New submission with subject: {{.Subject}}"

// Ensure Service implements the interface.
var _ domain.Mailer = (*Service)(nil)

var formSubmissionsCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mailbear_form_submissions_total",
		Help: "How many form submissions handled, partitioned by form name.",
	},
	[]string{"form"},
)

// formMailer bundles the resolved body template and compiled subject template for
// a single form.
type formMailer struct {
	body    *mailTemplate
	subject *textTmpl.Template
}

// Service is the concrete implementation of domain.Mailer.
type Service struct {
	logger       zerolog.Logger
	settings     domain.Settings
	forms        []*domain.Form
	httpClient   *http.Client
	templatesDir string
	mailers      map[string]*formMailer
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

// WithTemplatesDir sets a directory of external <name>.html / <name>.txt templates
// that override and extend the embedded default.
func WithTemplatesDir(dir string) Option {
	return func(s *Service) { s.templatesDir = dir }
}

// New builds a Service, loads templates, and validates the configuration.
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

	templates, err := loadTemplates(s.templatesDir)
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	s.mailers = make(map[string]*formMailer, len(s.forms))
	for _, form := range s.forms {
		templateName := form.Template
		if templateName == "" {
			templateName = defaultTemplateName
		}

		body, ok := templates[templateName]
		if !ok {
			return nil, fmt.Errorf("form %q references unknown template %q", form.Key, templateName)
		}

		subjectStr := form.Subject
		if subjectStr == "" {
			subjectStr = defaultSubjectTemplate
		}

		subject, err := textTmpl.New("subject:" + form.Key).Funcs(templateFuncs).Parse(subjectStr)
		if err != nil {
			return nil, fmt.Errorf("form %q has an invalid subject template: %w", form.Key, err)
		}

		s.mailers[form.Key] = &formMailer{body: body, subject: subject}
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

// Send renders the form's subject and body templates and delivers the email to the
// form's recipients. When the template has a text part, a multipart/alternative
// (text + HTML) message is sent; otherwise it is HTML-only.
func (s *Service) Send(_ context.Context, submission *domain.FormSubmission) error {
	form := s.FormByKey(submission.FormID)
	if form == nil {
		return fmt.Errorf("form does not exist")
	}

	mailer := s.mailers[form.Key]
	if mailer == nil {
		return fmt.Errorf("no mailer configured for form %q", form.Key)
	}

	data := domain.TemplateData{
		Name:     submission.Name,
		Email:    submission.Email,
		Subject:  submission.Subject,
		Content:  submission.Content,
		FormName: form.HumanReadableName,
	}

	var subjectBuf strings.Builder
	if err := mailer.subject.Execute(&subjectBuf, data); err != nil {
		return errors.Wrap(err, "couldn't render the subject")
	}

	htmlBody, textBody, err := mailer.body.render(data)
	if err != nil {
		return errors.Wrap(err, "couldn't render the email body")
	}

	msg := mail.NewMessage()
	msg.SetHeader("From", s.settings.SMTP.FromEmail)
	msg.SetHeader("To", form.ToEmail...)
	msg.SetAddressHeader("Reply-To", submission.Email, submission.Name)
	msg.SetHeader("Subject", subjectBuf.String())

	if textBody != "" {
		msg.SetBody("text/plain", textBody)
		msg.AddAlternative("text/html", htmlBody)
	} else {
		msg.SetBody("text/html", htmlBody)
	}

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
