package domain_test

import (
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestFormValidateValid(t *testing.T) {
	form := domain.Form{
		Key:            "test",
		AllowedDomains: []string{"allowed.tld", "another.allowed.tld"},
		ToEmail:        []string{"some@domain.tld"},
	}
	require.NoError(t, form.Validate(), "should be valid form")
}

func TestFormValidateInvalid(t *testing.T) {
	testInput := []domain.Form{
		{
			// no allowed domains
			Key:            "test",
			AllowedDomains: []string{},
			ToEmail:        []string{"some@domain.tld"},
		},
		{
			// no to email
			Key:            "test",
			AllowedDomains: []string{"allowed.tld"},
		},
		{
			// no form key
			AllowedDomains: []string{"allowed.tld"},
			ToEmail:        []string{"some@domain.tld"},
		},
		{
			// invalid email
			Key:            "test",
			AllowedDomains: []string{"allowed.tld"},
			ToEmail:        []string{"some-invalid-email"},
		},
	}

	for _, form := range testInput {
		require.Error(t, form.Validate(), "should be invalid form")
	}
}

func TestFormValidateWebhookOnly(t *testing.T) {
	form := domain.Form{
		Key:            "hook",
		AllowedDomains: []string{"example.com"},
		WebhookURL:     "https://hooks.example.com/abc",
	}
	require.NoError(t, form.Validate(), "a webhook-only form (no to_email) should be valid")
}

func TestFormValidateNoChannel(t *testing.T) {
	form := domain.Form{Key: "x", AllowedDomains: []string{"example.com"}}
	require.Error(t, form.Validate(), "a form with neither to_email nor webhook_url is invalid")
}

func TestFormValidateBadWebhookURL(t *testing.T) {
	form := domain.Form{
		Key:            "x",
		AllowedDomains: []string{"example.com"},
		WebhookURL:     "not-a-url",
	}
	require.Error(t, form.Validate(), "a non-http(s) webhook_url is invalid")
}

func TestFormValidateRedirectURLs(t *testing.T) {
	form := domain.Form{
		Key:              "x",
		AllowedDomains:   []string{"example.com"},
		ToEmail:          []string{"a@b.com"},
		RedirectURL:      "https://example.com/thanks",
		ErrorRedirectURL: "https://example.com/oops",
	}
	require.NoError(t, form.Validate(), "valid http(s) redirect URLs should pass")

	form.ErrorRedirectURL = "javascript:alert(1)"
	require.Error(t, form.Validate(), "a non-http(s) error_redirect_url is invalid")
}

func TestOriginDomainAllowed(t *testing.T) {
	form := domain.Form{AllowedDomains: []string{"allowed.tld", "another.allowed.tld"}}
	require.True(t, form.OriginDomainAllowed("http://allowed.tld"), "allowed domain")
	require.True(t, form.OriginDomainAllowed("https://another.allowed.tld"), "allowed domain")
	require.False(t, form.OriginDomainAllowed("http://random.domain.tld"), "not allowed domain")
	require.False(t, form.OriginDomainAllowed(""), "empty origin")
}

func TestOriginDomainWildcard(t *testing.T) {
	form := domain.Form{AllowedDomains: []string{"*"}}
	require.True(t, form.OriginDomainAllowed("http://anything.tld"), "wildcard allows any origin")
}

func TestFormSubmissionValid(t *testing.T) {
	testInput := []domain.FormSubmission{
		{
			Name:    "Foo Bar",
			Email:   "foo@bar",
			Subject: "Some Subject",
			Content: "Some Content\nwith newline",
			FormID:  "some-random-id",
		},
		{
			// without name (name is optional)
			Email:   "foo@bar",
			Subject: "Some Subject",
			Content: "Some Content",
			FormID:  "some-random-id",
		},
	}

	for _, sub := range testInput {
		require.NoError(t, sub.Validate(), "should be valid FormSubmission")
	}
}

func TestFormSubmissionInvalid(t *testing.T) {
	testInput := []domain.FormSubmission{
		{
			// missing id
			Email:   "foo@bar",
			Subject: "Some Subject",
			Content: "Some Content",
		},
		{
			// invalid email
			Email:   "foogegez",
			Subject: "Some Subject",
			Content: "Some Content",
			FormID:  "some-random-id",
		},
		{
			// subject not set
			Email:   "foo@bar",
			Content: "Some Content",
			FormID:  "some-random-id",
		},
		{
			// content not set
			Email:   "foo@bar",
			Subject: "Some Subject",
			FormID:  "some-random-id",
		},
	}

	for _, sub := range testInput {
		require.Error(t, sub.Validate(), "should be invalid FormSubmission")
	}
}
