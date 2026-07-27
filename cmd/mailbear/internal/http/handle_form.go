package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
)

func (s *Server) handleForm(w http.ResponseWriter, r *http.Request) {
	formID := chi.URLParam(r, "id")

	// check if form exists
	form := s.mailer.FormByKey(formID)
	if form == nil {
		writeJSON(w, http.StatusNotFound, "the given form does not exist")
		return
	}

	// parse form data from the body (JSON or form-encoded)
	data, err := bindSubmission(r, formID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	// validate form data
	if err := data.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	// honeypot: a legitimate front-end leaves this hidden field empty. If it's
	// filled, silently return success so bots don't learn the field is a trap,
	// but drop the submission without sending mail.
	if data.Honeypot != "" {
		s.logger.Warn().Str("form", data.FormID).Msg("honeypot triggered; dropping submission")
		writeJSON(w, http.StatusOK, "form was submitted successfully")
		return
	}

	// check if domain allowed (defense in depth; the Origin header is only
	// enforced by browsers, so this is not a substitute for the checks below)
	if !form.OriginDomainAllowed(r.Header.Get("Origin")) {
		writeJSON(w, http.StatusForbidden, "you're not allowed to send from this domain")
		return
	}

	// captcha: when Turnstile is configured, verify the token server-side. This
	// is the real gate against non-browser abuse, since a script can forge Origin.
	if s.mailer.TurnstileEnabled() {
		ok, err := s.mailer.VerifyTurnstile(r.Context(), data.TurnstileToken, realIP(r))
		if err != nil {
			s.logger.Error().Err(err).Str("form", data.FormID).Msg("turnstile verification failed to complete")
			writeJSON(w, http.StatusBadGateway, "couldn't verify the captcha")
			return
		}
		if !ok {
			writeJSON(w, http.StatusForbidden, "captcha verification failed")
			return
		}
	}

	// send the mail
	if err := s.mailer.Send(r.Context(), data); err != nil {
		s.logger.Error().Err(err).Str("form", data.FormID).Msg("failed to send form submission email")
		writeJSON(w, http.StatusInternalServerError, "couldn't send the mail")
		return
	}

	writeJSON(w, http.StatusOK, "form was submitted successfully")
}

// bindSubmission reads a submission from the request body, supporting both JSON
// and form-urlencoded payloads (matching what browser front-ends send).
func bindSubmission(r *http.Request, formID string) (*domain.FormSubmission, error) {
	data := &domain.FormSubmission{FormID: formID}

	contentType := r.Header.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}

	switch strings.TrimSpace(contentType) {
	case "application/json":
		if err := json.NewDecoder(r.Body).Decode(data); err != nil {
			return nil, err
		}
	default:
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		data.Name = r.PostForm.Get("name")
		data.Email = r.PostForm.Get("email")
		data.Subject = r.PostForm.Get("subject")
		data.Content = r.PostForm.Get("content")
		data.Honeypot = r.PostForm.Get("_gotcha")
		data.TurnstileToken = r.PostForm.Get("cf-turnstile-response")
	}

	return data, nil
}
