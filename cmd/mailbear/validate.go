package main

import (
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/config"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/logic"
	"github.com/spf13/cobra"
)

func newValidateCMD() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration and templates without starting the server",
		Long: `Run the same checks the server performs at startup — parse the forms config,
load and parse the templates, compile each form's subject, and (when any form
sends email) require the SMTP settings — then exit. Returns a non-zero status on
the first problem, so it can gate a CI or pre-deploy step.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			forms, err := config.LoadForms(configFile)
			if err != nil {
				return err
			}

			if _, err := logic.New(
				log,
				logic.WithSettings(settings()),
				logic.WithForms(forms),
				logic.WithTemplatesDir(templatesDir),
			); err != nil {
				return err
			}

			log.Info().Int("forms", len(forms)).Msg("Configuration is valid")
			return nil
		},
	}
}
