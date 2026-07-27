package logic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestDefaultTemplateRenderEscapes(t *testing.T) {
	templates, err := loadTemplates("")
	require.NoError(t, err)

	tmpl := templates[defaultTemplateName]
	require.NotNil(t, tmpl, "embedded default template must exist")

	html, text, err := tmpl.render(domain.TemplateData{
		Name:    "<script>alert(1)</script>",
		Email:   "a@b.com",
		Subject: "Hi & bye",
		Content: "line1\nline2 <b>bold</b>",
	})
	require.NoError(t, err)

	// HTML body is contextually escaped by html/template.
	require.NotContains(t, html, "<script>", "script tag must be escaped")
	require.Contains(t, html, "&lt;script&gt;")
	require.Contains(t, html, "Hi &amp; bye")
	require.Contains(t, html, "line1<br>line2", "nl2br turns newlines into <br> after escaping")
	require.NotContains(t, html, "<b>bold</b>", "user content must not inject raw HTML")

	// Default has a .txt part, rendered raw (plain text needs no escaping).
	require.NotEmpty(t, text)
	require.Contains(t, text, "line2 <b>bold</b>")
}

func TestLoadExternalDirOverrideAndHTMLOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "custom.html"), []byte("<p>{{ .Name }}</p>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.html"), []byte("OVERRIDDEN {{ .Name }}"), 0o600))

	templates, err := loadTemplates(dir)
	require.NoError(t, err)

	// HTML-only template has no text part.
	custom := templates["custom"]
	require.NotNil(t, custom)
	require.Nil(t, custom.text)
	html, text, err := custom.render(domain.TemplateData{Name: "Joe"})
	require.NoError(t, err)
	require.Equal(t, "<p>Joe</p>", html)
	require.Empty(t, text)

	// External default.html overrides the embedded one.
	def := templates[defaultTemplateName]
	require.NotNil(t, def)
	html, _, err = def.render(domain.TemplateData{Name: "Joe"})
	require.NoError(t, err)
	require.Contains(t, html, "OVERRIDDEN Joe")
}

func TestLoadMissingDirErrors(t *testing.T) {
	_, err := loadTemplates(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err, "an explicitly-set but unreadable templates dir should error")
}
