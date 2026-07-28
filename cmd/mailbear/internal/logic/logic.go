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

// defaultAutoresponderSubject is used when an autoresponder sets no subject.
const defaultAutoresponderSubject = "Thanks for your submission"

// Ensure Service implements the interface.
var _ domain.Mailer = (*Service)(nil)

var formSubmissionsCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mailbear_form_submissions_total",
		Help: "How many form submissions handled, partitioned by form name.",
	},
	[]string{"form"},
)

var autoresponderCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mailbear_autoresponder_deliveries_total",
		Help: "Autoresponder emails handled, partitioned by form name and outcome.",
	},
	[]string{"form", "outcome"},
)

// formMailer bundles the resolved templates for a single form: the owner
// notification body/subject and, optionally, the submitter autoresponder.
type formMailer struct {
	body          *mailTemplate
	subject       *textTmpl.Template
	autoresponder *autoresponder
}

// autoresponder holds the resolved templates for the submitter confirmation email.
type autoresponder struct {
	body    *mailTemplate
	subject *textTmpl.Template
}

// emailSpec is a single email to render and deliver.
type emailSpec struct {
	to          []string
	replyToAddr string
	replyToName string
	subject     *textTmpl.Template
	body        *mailTemplate
}

// Service is the concrete implementation of domain.Mailer.
type Service struct {
	logger       zerolog.Logger
	settings     domain.Settings
	forms        []*domain.Form
	httpClient   *http.Client
	templatesDir string
	mailers      map[string]*formMailer
	store        domain.Store
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

// WithStore sets a store that records every submission and its delivery outcome.
func WithStore(store domain.Store) Option {
	return func(s *Service) { s.store = store }
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

	if len(s.forms) == 0 {
		return nil, fmt.Errorf("expected to have at least one form")
	}

	// SMTP is only required when at least one form actually delivers by email;
	// a webhook-only deployment needs no mail server.
	needsEmail := false
	for _, form := range s.forms {
		if len(form.ToEmail) > 0 || form.Autoresponder != nil {
			needsEmail = true
			break
		}
	}
	if needsEmail {
		if s.settings.SMTP.Host == "" {
			return nil, fmt.Errorf("smtp host must be set")
		}
		if s.settings.SMTP.Port == 0 {
			return nil, fmt.Errorf("smtp port must be set")
		}
		if s.settings.SMTP.FromEmail == "" {
			return nil, fmt.Errorf("smtp from email must be set")
		}
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

		mailer := &formMailer{body: body, subject: subject}

		if form.Autoresponder != nil {
			arBody, ok := templates[form.Autoresponder.Template]
			if !ok {
				return nil, fmt.Errorf("form %q autoresponder references unknown template %q", form.Key, form.Autoresponder.Template)
			}

			arSubjectStr := form.Autoresponder.Subject
			if arSubjectStr == "" {
				arSubjectStr = defaultAutoresponderSubject
			}

			arSubject, err := textTmpl.New("ar-subject:" + form.Key).Funcs(templateFuncs).Parse(arSubjectStr)
			if err != nil {
				return nil, fmt.Errorf("form %q has an invalid autoresponder subject template: %w", form.Key, err)
			}

			mailer.autoresponder = &autoresponder{body: arBody, subject: arSubject}
		}

		s.mailers[form.Key] = mailer
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

// Send delivers a submission over the form's configured channels: email (when
// to_email is set) and/or a webhook (when webhook_url is set). Email is the
// primary channel — when it is configured, a failure fails the whole request.
// A webhook is required only when it is the sole channel; otherwise a webhook
// failure is logged but does not fail a submission that already went out by email.
func (s *Service) Send(ctx context.Context, submission *domain.FormSubmission) (err error) {
	form := s.FormByKey(submission.FormID)
	if form == nil {
		return fmt.Errorf("form does not exist")
	}

	mailer := s.mailers[form.Key]
	if mailer == nil {
		return fmt.Errorf("no mailer configured for form %q", form.Key)
	}

	// Record the submission and its delivery outcome, regardless of success, so a
	// failed delivery still leaves a replayable record. Best-effort: an audit-log
	// write failure is logged but never fails the submission.
	if s.store != nil {
		defer func() {
			record := domain.SubmissionRecord{
				Timestamp: time.Now().UTC(),
				Form:      form.HumanReadableName,
				Name:      submission.Name,
				Email:     submission.Email,
				Subject:   submission.Subject,
				Content:   submission.Content,
				Delivered: err == nil,
			}
			if serr := s.store.Save(record); serr != nil {
				s.logger.Error().Err(serr).Str("form", form.HumanReadableName).Msg("failed to persist submission to audit log")
			}
		}()
	}

	data := templateData(form, submission)

	hasEmail := len(form.ToEmail) > 0
	hasWebhook := form.WebhookURL != ""

	if hasEmail {
		if err := s.sendEmail(form, mailer, data, submission); err != nil {
			return err
		}
		formSubmissionsCounter.With(prometheus.Labels{"form": form.HumanReadableName}).Add(1)
	}

	if hasWebhook {
		if err := s.sendWebhook(ctx, form, submission); err != nil {
			webhookDeliveriesCounter.With(prometheus.Labels{"form": form.HumanReadableName, "outcome": "failure"}).Add(1)
			if hasEmail {
				// The email already went out — don't fail the request over a
				// best-effort secondary channel.
				s.logger.Error().Err(err).Str("form", form.HumanReadableName).Msg("webhook delivery failed")
			} else {
				return err
			}
		} else {
			webhookDeliveriesCounter.With(prometheus.Labels{"form": form.HumanReadableName, "outcome": "success"}).Add(1)
			if !hasEmail {
				formSubmissionsCounter.With(prometheus.Labels{"form": form.HumanReadableName}).Add(1)
			}
		}
	}

	// Autoresponder: a courtesy confirmation to the submitter. Best-effort — the
	// submission already reached the owner, so a failure must not fail the
	// request (which would prompt a resubmit and re-notify the owner).
	if mailer.autoresponder != nil {
		if err := s.sendAutoresponder(form, mailer.autoresponder, data, submission); err != nil {
			autoresponderCounter.With(prometheus.Labels{"form": form.HumanReadableName, "outcome": "failure"}).Add(1)
			s.logger.Error().Err(err).Str("form", form.HumanReadableName).Msg("autoresponder delivery failed")
		} else {
			autoresponderCounter.With(prometheus.Labels{"form": form.HumanReadableName, "outcome": "success"}).Add(1)
		}
	}

	return nil
}

// templateData builds the values exposed to subject and body templates.
func templateData(form *domain.Form, submission *domain.FormSubmission) domain.TemplateData {
	return domain.TemplateData{
		Name:     submission.Name,
		Email:    submission.Email,
		Subject:  submission.Subject,
		Content:  submission.Content,
		FormName: form.HumanReadableName,
	}
}

// sendEmail delivers the owner-notification email to the form's recipients, with
// Reply-To set to the submitter so a reply reaches them.
func (s *Service) sendEmail(form *domain.Form, mailer *formMailer, data domain.TemplateData, submission *domain.FormSubmission) error {
	return s.deliver(emailSpec{
		to:          form.ToEmail,
		replyToAddr: submission.Email,
		replyToName: submission.Name,
		subject:     mailer.subject,
		body:        mailer.body,
	}, data)
}

// sendAutoresponder delivers the confirmation email to the submitter. Reply-To is
// set to the form's first recipient (when any) so replies reach the owner.
func (s *Service) sendAutoresponder(form *domain.Form, ar *autoresponder, data domain.TemplateData, submission *domain.FormSubmission) error {
	spec := emailSpec{
		to:      []string{submission.Email},
		subject: ar.subject,
		body:    ar.body,
	}
	if len(form.ToEmail) > 0 {
		spec.replyToAddr = form.ToEmail[0]
	}
	return s.deliver(spec, data)
}

// deliver renders and sends a single email. When the body template has a text
// part, a multipart/alternative (text + HTML) message is sent; otherwise HTML-only.
func (s *Service) deliver(spec emailSpec, data domain.TemplateData) error {
	var subjectBuf strings.Builder
	if err := spec.subject.Execute(&subjectBuf, data); err != nil {
		return errors.Wrap(err, "couldn't render the subject")
	}

	htmlBody, textBody, err := spec.body.render(data)
	if err != nil {
		return errors.Wrap(err, "couldn't render the email body")
	}

	msg := mail.NewMessage()
	msg.SetHeader("From", s.settings.SMTP.FromEmail)
	msg.SetHeader("To", spec.to...)
	if spec.replyToAddr != "" {
		msg.SetAddressHeader("Reply-To", spec.replyToAddr, spec.replyToName)
	}
	msg.SetHeader("Subject", subjectBuf.String())

	if textBody != "" {
		msg.SetBody("text/plain", textBody)
		msg.AddAlternative("text/html", htmlBody)
	} else {
		msg.SetBody("text/html", htmlBody)
	}

	if err := s.dialer().DialAndSend(msg); err != nil {
		return errors.Wrap(err, "couldn't send the email")
	}

	return nil
}

// dialer builds an SMTP dialer from the configured settings.
func (s *Service) dialer() *mail.Dialer {
	d := mail.NewDialer(
		s.settings.SMTP.Host,
		s.settings.SMTP.Port,
		s.settings.SMTP.User,
		s.settings.SMTP.Password,
	)
	if s.settings.SMTP.DisableTLS {
		d.StartTLSPolicy = mail.NoStartTLS
	}
	return d
}
