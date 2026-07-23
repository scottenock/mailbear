package mailbear

import (
	"fmt"

	"github.com/badoux/checkmail"
)

// FormSubmission represent a submitted form
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

// Validate validates all fields of a form submission.
func (f *FormSubmission) Validate() error {
	// Note: Name is optional and intentionally not validated here.
	if f.Email == "" {
		return fmt.Errorf("field 'email'cannot be empty")
	}
	if f.Subject == "" {
		return fmt.Errorf("field 'subject' cannot be empty")
	}
	if f.Content == "" {
		return fmt.Errorf("field 'content' cannot be empty")
	}

	// Validate ID, although it should always be set manually
	if f.FormID == "" {
		return fmt.Errorf("id cannot be empty")
	}

	// Validate format of email
	err := checkmail.ValidateFormat(f.Email)
	if err != nil {
		return fmt.Errorf("invalid email address: %v", err)
	}

	return nil
}
