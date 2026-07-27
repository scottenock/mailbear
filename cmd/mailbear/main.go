package main

import (
	"os"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/rs/zerolog"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=<tag>".
var version = "DEV"

var (
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Persistent flags.
	configFile   string
	templatesDir string
	httpAddress  string
	metricsAddr  string
	rateLimit    int
	prettyLog    bool
	logLevel     string
	smtpHost     string
	smtpPort     int
	smtpUser     string
	smtpPassword string
	smtpDisTLS   bool
	smtpFrom     string
	smtpFromName string
	turnstile    string
)

func main() {
	rootCMD := newRootCMD()
	rootCMD.AddCommand(newServeCMD())

	if err := rootCMD.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mailbear",
		Short:   "MailBear - a self-hosted forms backend",
		Version: version,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			level, err := zerolog.ParseLevel(logLevel)
			if err != nil {
				level = zerolog.InfoLevel
			}

			if prettyLog {
				log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(level).With().Timestamp().Logger()
			} else {
				log = zerolog.New(os.Stderr).Level(level).With().Timestamp().Logger()
			}
		},
	}

	f := cmd.PersistentFlags()
	f.StringVar(&configFile, "config", getEnvFallback("CONFIG_FILE", "config.yml"), "Path to the forms config file")
	f.StringVar(&templatesDir, "templatesDir", getEnvFallback("TEMPLATES_DIR", ""), "Directory of custom email templates (<name>.html / <name>.txt); empty uses the built-in default")
	f.StringVar(&httpAddress, "httpAddress", getEnvFallback("HTTP_ADDRESS", ":1234"), "Address the API server listens on")
	f.StringVar(&metricsAddr, "metricsAddress", getEnvFallback("METRICS_ADDRESS", ":9090"), "Address the Prometheus metrics server listens on")
	f.IntVar(&rateLimit, "rateLimit", cast.ToInt(getEnvFallback("RATE_LIMIT", "5")), "Max submissions per client IP per minute")
	f.BoolVar(&prettyLog, "prettyLog", cast.ToBool(getEnvFallback("PRETTY_LOG", "true")), "Pretty-print logs to the console")
	f.StringVar(&logLevel, "logLevel", getEnvFallback("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	f.StringVar(&smtpHost, "smtpHost", getEnvFallback("SMTP_HOST", ""), "SMTP server hostname")
	f.IntVar(&smtpPort, "smtpPort", cast.ToInt(getEnvFallback("SMTP_PORT", "587")), "SMTP server port")
	f.StringVar(&smtpUser, "smtpUser", getEnvFallback("SMTP_USER", ""), "SMTP username")
	f.StringVar(&smtpPassword, "smtpPassword", getEnvFallback("SMTP_PASSWORD", ""), "SMTP password")
	f.BoolVar(&smtpDisTLS, "smtpDisableTLS", cast.ToBool(getEnvFallback("SMTP_DISABLE_TLS", "false")), "Disable STARTTLS for the SMTP connection")
	f.StringVar(&smtpFrom, "smtpFromEmail", getEnvFallback("SMTP_FROM_EMAIL", ""), "From address for outgoing mail")
	f.StringVar(&smtpFromName, "smtpFromName", getEnvFallback("SMTP_FROM_NAME", "MailBear"), "From name for outgoing mail")
	f.StringVar(&turnstile, "turnstileSecret", getEnvFallback("TURNSTILE_SECRET", ""), "Cloudflare Turnstile secret key (empty disables captcha)")

	return cmd
}

// settings assembles the operational settings from the parsed flags/env.
func settings() domain.Settings {
	return domain.Settings{
		SMTP: domain.SMTP{
			Host:       smtpHost,
			Port:       smtpPort,
			User:       smtpUser,
			Password:   smtpPassword,
			DisableTLS: smtpDisTLS,
			FromEmail:  smtpFrom,
			FromName:   smtpFromName,
		},
		TurnstileSecret: turnstile,
	}
}

func getEnvFallback(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
