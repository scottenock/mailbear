package http

import (
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Outcome labels for mailbear_form_requests_total.
const (
	resultSuccess         = "success"
	resultHoneypot        = "honeypot"
	resultInvalid         = "invalid"
	resultForbiddenOrigin = "forbidden_origin"
	resultCaptchaFailed   = "captcha_failed"
	resultCaptchaError    = "captcha_error"
	resultSendError       = "send_error"
	resultNotFound        = "not_found"
)

var (
	formRequestsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailbear_form_requests_total",
		Help: "Form submission requests handled, partitioned by form name and outcome.",
	}, []string{"form", "result"})

	rateLimitedCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailbear_rate_limited_total",
		Help: "Requests rejected by the rate limiter.",
	})
)

// recordResult increments the request-outcome counter. For an unknown form (no
// form object), a fixed "unknown" label is used to avoid unbounded cardinality
// from attacker-supplied form keys.
func recordResult(form *domain.Form, result string) {
	name := "unknown"
	if form != nil {
		name = form.HumanReadableName
	}
	formRequestsCounter.With(prometheus.Labels{"form": name, "result": result}).Inc()
}
