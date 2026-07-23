// Package config loads and validates the forms configuration file. Operational
// settings (SMTP, addresses, Turnstile) come from flags/env, not this file — only
// the per-form list lives here.
package config

import (
	"fmt"
	"os"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// file is the on-disk shape of the forms config.
type file struct {
	Forms map[string]*domain.Form `yaml:"forms"`
}

// LoadForms parses and validates the forms config file, returning the configured
// forms. Each form's HumanReadableName is set from its map key.
func LoadForms(path string) ([]*domain.Form, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't open config file %q", path)
	}

	var parsed file
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.Wrap(err, "couldn't parse config file")
	}

	if len(parsed.Forms) == 0 {
		return nil, fmt.Errorf("expected to have at least one form")
	}

	forms := make([]*domain.Form, 0, len(parsed.Forms))
	seenKeys := make(map[string]bool, len(parsed.Forms))

	for name, form := range parsed.Forms {
		form.HumanReadableName = name

		if err := form.Validate(); err != nil {
			return nil, fmt.Errorf("invalid form with key %q: %v", form.Key, err)
		}
		if seenKeys[form.Key] {
			return nil, fmt.Errorf("there already exists a form with the key: %q", form.Key)
		}
		seenKeys[form.Key] = true

		forms = append(forms, form)
	}

	return forms, nil
}
