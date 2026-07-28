package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/config"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/http"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/logic"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/store"
	"github.com/spf13/cobra"
)

// shutdownTimeout bounds how long in-flight requests have to drain on shutdown.
const shutdownTimeout = 15 * time.Second

func newServeCMD() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the MailBear server",
		Run: func(cmd *cobra.Command, _ []string) {
			forms, err := config.LoadForms(configFile)
			if err != nil {
				log.Fatal().Err(err).Msg("Could not load forms config")
			}

			opts := []logic.Option{
				logic.WithSettings(settings()),
				logic.WithForms(forms),
				logic.WithTemplatesDir(templatesDir),
			}

			if auditLog != "" {
				auditStore, err := store.NewFile(store.Options{
					Path:       auditLog,
					MaxSizeMB:  auditLogMaxSizeMB,
					MaxBackups: auditLogMaxBackups,
					MaxAgeDays: auditLogMaxAgeDays,
					Compress:   auditLogCompress,
				})
				if err != nil {
					log.Fatal().Err(err).Msg("Could not open audit log")
				}
				defer func() {
					if cerr := auditStore.Close(); cerr != nil {
						log.Error().Err(cerr).Msg("Failed to close audit log")
					}
				}()
				opts = append(opts, logic.WithStore(auditStore))
			}

			mailer, err := logic.New(log, opts...)
			if err != nil {
				log.Fatal().Err(err).Msg("Could not initialize mailer")
			}

			if reservedField(honeypotField) {
				log.Fatal().Str("honeypotField", honeypotField).Msg("honeypotField collides with a real submission field")
			}

			server := http.New(log, mailer, httpAddress, metricsAddr, rateLimit, version, honeypotField)

			runWithGracefulShutdown(cmd.Context(), func() {
				if err := server.Serve(); err != nil {
					log.Error().Err(err).Msg("Received error from HTTP server")
				}
			})

			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()

			if err := server.Shutdown(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to shut down HTTP server")
			}
		},
	}
}

// reservedField reports whether name clashes with a real submission field, which
// would make the honeypot silently drop every legitimate submission.
func reservedField(name string) bool {
	switch name {
	case "name", "email", "subject", "content", "cf-turnstile-response":
		return true
	default:
		return false
	}
}

// runWithGracefulShutdown runs fn until it returns or an interrupt/SIGTERM arrives.
func runWithGracefulShutdown(_ context.Context, fn func()) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-stop:
		log.Info().Msg("Shutting down...")
	case <-done:
		// Server stopped on its own.
	}
}
