package logic

import (
	"embed"
	"fmt"
	htmlTmpl "html/template"
	"os"
	"path/filepath"
	"strings"
	textTmpl "text/template"
)

//go:embed templates
var embeddedTemplates embed.FS

// defaultTemplateName is the built-in template used by forms that don't set one.
const defaultTemplateName = "default"

// templateFuncs are helpers available to every template. The map type is
// assignable to both html/template.FuncMap and text/template.FuncMap.
var templateFuncs = map[string]any{
	// nl2br HTML-escapes s and converts newlines to <br>. Escaping first keeps the
	// output injection-safe while preserving line breaks in the HTML body.
	"nl2br": func(s string) htmlTmpl.HTML {
		return htmlTmpl.HTML(strings.ReplaceAll(htmlTmpl.HTMLEscapeString(s), "\n", "<br>")) //nolint:gosec // input is escaped above
	},
}

// mailTemplate is a parsed body template. text may be nil (HTML-only template).
type mailTemplate struct {
	name string
	html *htmlTmpl.Template
	text *textTmpl.Template
}

// render executes the HTML and (optional) text templates against data. The
// returned text is empty when the template has no .txt part.
func (t *mailTemplate) render(data any) (html, text string, err error) {
	var htmlBuf strings.Builder
	if err := t.html.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("render html template %q: %w", t.name, err)
	}

	if t.text == nil {
		return htmlBuf.String(), "", nil
	}

	var textBuf strings.Builder
	if err := t.text.Execute(&textBuf, data); err != nil {
		return "", "", fmt.Errorf("render text template %q: %w", t.name, err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

// loadTemplates parses the embedded default template, then merges/overrides with
// any <name>.html / <name>.txt pairs found in dir (when dir is non-empty). Every
// resulting template must have an .html part; a .txt part is optional.
func loadTemplates(dir string) (map[string]*mailTemplate, error) {
	templates := map[string]*mailTemplate{}

	entries, err := embeddedTemplates.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read embedded templates: %w", err)
	}
	for _, entry := range entries {
		if err := addTemplateFile(templates, entry.Name(), func() ([]byte, error) {
			return embeddedTemplates.ReadFile("templates/" + entry.Name())
		}); err != nil {
			return nil, err
		}
	}

	if dir != "" {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read templates dir %q: %w", dir, err)
		}
		for _, entry := range dirEntries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if err := addTemplateFile(templates, name, func() ([]byte, error) {
				return os.ReadFile(filepath.Join(dir, name))
			}); err != nil {
				return nil, err
			}
		}
	}

	for name, t := range templates {
		if t.html == nil {
			return nil, fmt.Errorf("template %q is missing a .html file", name)
		}
	}

	return templates, nil
}

// addTemplateFile parses a single template file (identified by its extension) and
// attaches it to the named entry in templates, creating the entry if needed.
// Non-template files (anything other than .html/.txt) are ignored.
func addTemplateFile(templates map[string]*mailTemplate, filename string, read func() ([]byte, error)) error {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	if name == "" || (ext != ".html" && ext != ".txt") {
		return nil
	}

	data, err := read()
	if err != nil {
		return fmt.Errorf("read template %q: %w", filename, err)
	}

	t := templates[name]
	if t == nil {
		t = &mailTemplate{name: name}
		templates[name] = t
	}

	switch ext {
	case ".html":
		parsed, err := htmlTmpl.New(name).Funcs(templateFuncs).Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse html template %q: %w", filename, err)
		}
		t.html = parsed
	case ".txt":
		parsed, err := textTmpl.New(name).Funcs(templateFuncs).Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse text template %q: %w", filename, err)
		}
		t.text = parsed
	}

	return nil
}
