package config_test

import (
	"testing"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadFormsValid(t *testing.T) {
	forms, err := config.LoadForms("testdata/forms.yml")
	require.NoError(t, err, "should be able to parse a valid forms file")
	require.Len(t, forms, 1, "sample file has a single form")
	require.Equal(t, "some-random-key", forms[0].Key)
	require.Equal(t, "some-form-name", forms[0].HumanReadableName, "human-readable name comes from the map key")
}

func TestLoadFormsNotExists(t *testing.T) {
	_, err := config.LoadForms("testdata/does-not-exist.yml")
	require.Error(t, err, "should error on a missing file")
}

func TestLoadFormsInvalid(t *testing.T) {
	_, err := config.LoadForms("testdata/invalid.yml")
	require.Error(t, err, "should error on malformed yaml")
}
