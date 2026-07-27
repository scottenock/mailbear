package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeForms(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "forms.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestValidateWebhookOnly(t *testing.T) {
	configFile = writeForms(t, "forms:\n  a:\n    key: k\n    allowed_domains: [example.com]\n    webhook_url: https://example.com/h\n")
	templatesDir = ""

	cmd := newValidateCMD()
	require.NoError(t, cmd.RunE(cmd, nil), "webhook-only config with no SMTP should validate")
}

func TestValidateEmailNeedsSMTP(t *testing.T) {
	configFile = writeForms(t, "forms:\n  a:\n    key: k\n    allowed_domains: [example.com]\n    to_email: [me@example.com]\n")
	templatesDir = ""
	smtpHost = ""
	smtpFrom = ""

	cmd := newValidateCMD()
	require.Error(t, cmd.RunE(cmd, nil), "email form without SMTP settings should fail validation")
}

func TestValidateMissingConfig(t *testing.T) {
	configFile = filepath.Join(t.TempDir(), "does-not-exist.yml")

	cmd := newValidateCMD()
	require.Error(t, cmd.RunE(cmd, nil), "missing config file should fail validation")
}
