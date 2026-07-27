// Package domain defines the core models and the Mailer interface that the
// transport (http) layer depends on. It has no dependencies on other layers.
package domain

import (
	"context"
	"fmt"
	"net/url"

	"github.com/badoux/checkmail"
)

// Mailer is the business-logic contract consumed by the HTTP layer. The concrete
// implementation lives in the logic package.
type Mailer interface {
	// FormByKey returns the configured form with the given key, or nil if none exists.
	FormByKey(key string) *Form

	// TurnstileEnabled reports whether Cloudflare Turnstile verification is configured.
	TurnstileEnabled() bool

	// VerifyTurnstile validates a Turnstile response token. remoteIP is optional.
	VerifyTurnstile(ctx context.Context, token, remoteIP string) (bool, error)

	// Send delivers a validated form submission to the form's recipients.
	Send(ctx context.Context, submission *FormSubmission) error
}

// SMTP holds the outbound mail server settings.
type SMTP struct {
	Host       string
	Port       int
	User       string
	Password   string
	DisableTLS bool
	FromEmail  string
	FromName   string
}

// Settings holds the operational configuration supplied via flags/env.
type Settings struct {
	SMTP            SMTP
	TurnstileSecret string
}

// Form represents a single configured form.
type Form struct {
	// HumanReadableName is set while parsing the config, from the key of the forms map.
	HumanReadableName string   `yaml:"-"`
	Key               string   `yaml:"key"`
	AllowedDomains    []string `yaml:"allowed_domains"`
	ToEmail           []string `yaml:"to_email"`

	// Template is the name of the body template to use (a <name>.html / <name>.txt
	// pair). Empty means the built-in "default" template.
	Template string `yaml:"template"`

	// Subject is an optional text/template string for the email subject line. Empty
	// means the default subject ("New submission with subject: {{.Subject}}").
	Subject string `yaml:"subject"`

	// WebhookURL, when set, receives each submission as a JSON POST. A form may
	// configure to_email, webhook_url, or both.
	WebhookURL string `yaml:"webhook_url"`
}

// TemplateData is the data made available to subject and body templates.
type TemplateData struct {
	Name     string
	Email    string
	Subject  string
	Content  string
	FormName string
}

// Validate checks that a form has the required values.
func (form *Form) Validate() error {
	if form.Key == "" {
		return fmt.Errorf("'key' of form cannot be empty")
	}
	if len(form.AllowedDomains) == 0 {
		return fmt.Errorf("form should have at least one allowed domain in 'allowed_domains'")
	}
	if len(form.ToEmail) == 0 && form.WebhookURL == "" {
		return fmt.Errorf("form must have at least one recipient in 'to_email' or a 'webhook_url'")
	}
	for _, email := range form.ToEmail {
		if err := checkmail.ValidateFormat(email); err != nil {
			return fmt.Errorf("invalid email address %q in 'to_email'", email)
		}
	}
	if form.WebhookURL != "" {
		u, err := url.Parse(form.WebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid 'webhook_url' %q: must be an http(s) URL", form.WebhookURL)
		}
	}
	return nil
}

// OriginDomainAllowed reports whether the given Origin header is allowed to submit
// this form. This is a browser-enforced hint only (Origin is trivially forged by
// non-browser clients), so it is defense-in-depth, not a real access control.
func (form *Form) OriginDomainAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originDomain := u.Host

	for _, allowedDomain := range form.AllowedDomains {
		if allowedDomain == "*" {
			return true
		}
		if allowedDomain == originDomain {
			return true
		}
	}
	return false
}

// FormSubmission represents a submitted form.
type FormSubmission struct {
	Name    string `json:"name" form:"name"`
	Email   string `json:"email" form:"email"`
	Subject string `json:"subject" form:"subject"`
	Content string `json:"content" form:"content"`
	FormID  string `json:"-"`

	// Honeypot is a decoy field that legitimate front-ends keep hidden and empty.
	// Naive bots fill every field, so a non-empty value marks the submission as spam.
	Honeypot string `json:"_gotcha" form:"_gotcha"`

	// TurnstileToken carries the Cloudflare Turnstile response token produced by the
	// widget on the client. It is verified server-side when a Turnstile secret is set.
	TurnstileToken string `json:"cf-turnstile-response" form:"cf-turnstile-response"`
}

// Validate validates the user-supplied fields of a form submission.
func (f *FormSubmission) Validate() error {
	// Note: Name is optional and intentionally not validated here.
	if f.Email == "" {
		return fmt.Errorf("field 'email' cannot be empty")
	}
	if f.Subject == "" {
		return fmt.Errorf("field 'subject' cannot be empty")
	}
	if f.Content == "" {
		return fmt.Errorf("field 'content' cannot be empty")
	}

	// Validate ID, although it should always be set manually.
	if f.FormID == "" {
		return fmt.Errorf("id cannot be empty")
	}

	if err := checkmail.ValidateFormat(f.Email); err != nil {
		return fmt.Errorf("invalid email address: %v", err)
	}

	return nil
}
