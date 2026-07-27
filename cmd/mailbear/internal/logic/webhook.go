package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var webhookDeliveriesCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "mailbear_webhook_deliveries_total",
		Help: "Webhook deliveries handled, partitioned by form name and outcome.",
	},
	[]string{"form", "outcome"},
)

// webhookPayload is the JSON body POSTed to a form's webhook_url.
type webhookPayload struct {
	Form    string `json:"form"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Subject string `json:"subject"`
	Content string `json:"content"`
}

// sendWebhook POSTs the submission to the form's webhook URL as JSON. The URL is
// operator-configured (from the config file, never user input), so it is trusted
// and not subject to SSRF filtering.
func (s *Service) sendWebhook(ctx context.Context, form *domain.Form, submission *domain.FormSubmission) error {
	body, err := json.Marshal(webhookPayload{
		Form:    form.HumanReadableName,
		Name:    submission.Name,
		Email:   submission.Email,
		Subject: submission.Subject,
		Content: submission.Content,
	})
	if err != nil {
		return errors.Wrap(err, "couldn't encode webhook payload")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, form.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "couldn't build webhook request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "couldn't reach webhook")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
