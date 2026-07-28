package http

import (
	"encoding/json"
	"io"
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
		recordResult(nil, resultNotFound)
		writeJSON(w, http.StatusNotFound, "the given form does not exist")
		return
	}

	// parse form data from the body (JSON or form-encoded)
	data, err := bindSubmission(r, formID, s.honeypotField)
	if err != nil {
		recordResult(form, resultInvalid)
		respondError(w, r, form, http.StatusBadRequest, err.Error())
		return
	}

	// validate form data
	if err := data.Validate(); err != nil {
		recordResult(form, resultInvalid)
		respondError(w, r, form, http.StatusBadRequest, err.Error())
		return
	}

	// honeypot: a legitimate front-end leaves this hidden field empty. If it's
	// filled, silently return success so bots don't learn the field is a trap,
	// but drop the submission without sending mail.
	if data.Honeypot != "" {
		s.logger.Warn().Str("form", data.FormID).Msg("honeypot triggered; dropping submission")
		recordResult(form, resultHoneypot)
		respondSuccess(w, r, form)
		return
	}

	// check if domain allowed (defense in depth; the Origin header is only
	// enforced by browsers, so this is not a substitute for the checks below)
	if !form.OriginDomainAllowed(r.Header.Get("Origin")) {
		recordResult(form, resultForbiddenOrigin)
		respondError(w, r, form, http.StatusForbidden, "you're not allowed to send from this domain")
		return
	}

	// captcha: when Turnstile is configured, verify the token server-side. This
	// is the real gate against non-browser abuse, since a script can forge Origin.
	if s.mailer.TurnstileEnabled() {
		ok, err := s.mailer.VerifyTurnstile(r.Context(), data.TurnstileToken, realIP(r))
		if err != nil {
			s.logger.Error().Err(err).Str("form", data.FormID).Msg("turnstile verification failed to complete")
			recordResult(form, resultCaptchaError)
			respondError(w, r, form, http.StatusBadGateway, "couldn't verify the captcha")
			return
		}
		if !ok {
			recordResult(form, resultCaptchaFailed)
			respondError(w, r, form, http.StatusForbidden, "captcha verification failed")
			return
		}
	}

	// send the mail
	if err := s.mailer.Send(r.Context(), data); err != nil {
		s.logger.Error().Err(err).Str("form", data.FormID).Msg("failed to send form submission email")
		recordResult(form, resultSendError)
		respondError(w, r, form, http.StatusInternalServerError, "couldn't send the mail")
		return
	}

	recordResult(form, resultSuccess)
	respondSuccess(w, r, form)
}

// respondSuccess redirects a browser form post to the form's redirect_url (303)
// when configured; otherwise it returns the JSON success body.
func respondSuccess(w http.ResponseWriter, r *http.Request, form *domain.Form) {
	if form.RedirectURL != "" && isFormPost(r) {
		http.Redirect(w, r, form.RedirectURL, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, "form was submitted successfully")
}

// respondError redirects a browser form post to the form's error_redirect_url
// (303) when configured; otherwise it returns the JSON error body.
func respondError(w http.ResponseWriter, r *http.Request, form *domain.Form, status int, message string) {
	if form.ErrorRedirectURL != "" && isFormPost(r) {
		http.Redirect(w, r, form.ErrorRedirectURL, http.StatusSeeOther)
		return
	}
	writeJSON(w, status, message)
}

// isFormPost reports whether the request looks like a plain browser form
// submission (rather than a JSON/AJAX request), which is what redirects target.
func isFormPost(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}
	switch strings.TrimSpace(contentType) {
	case "application/x-www-form-urlencoded", "multipart/form-data":
		return true
	default:
		return false
	}
}

// bindSubmission reads a submission from the request body, supporting both JSON
// and form-urlencoded payloads (matching what browser front-ends send). The
// honeypot value is read from honeypotField, whose name is configurable.
func bindSubmission(r *http.Request, formID, honeypotField string) (*domain.FormSubmission, error) {
	data := &domain.FormSubmission{FormID: formID}

	contentType := r.Header.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}

	switch strings.TrimSpace(contentType) {
	case "application/json":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, data); err != nil {
			return nil, err
		}
		data.Honeypot = jsonStringField(body, honeypotField)
	default:
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		data.Name = r.PostForm.Get("name")
		data.Email = r.PostForm.Get("email")
		data.Subject = r.PostForm.Get("subject")
		data.Content = r.PostForm.Get("content")
		data.TurnstileToken = r.PostForm.Get("cf-turnstile-response")
		data.Honeypot = r.PostForm.Get(honeypotField)
	}

	return data, nil
}

// jsonStringField extracts a single string field by name from a JSON object,
// returning "" if the field is absent or not a string.
func jsonStringField(body []byte, name string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	value, ok := raw[name]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(value, &s)
	return s
}
