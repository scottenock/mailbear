package http

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
)

func (s *Server) handleForm(c echo.Context) error {
	formID := c.Param("id")

	// check if form exists
	form := s.mailer.FormByKey(formID)
	if form == nil {
		return c.JSON(http.StatusNotFound, response("the given form does not exist"))
	}

	// get form data from body
	data := &domain.FormSubmission{FormID: formID}
	if err := c.Bind(data); err != nil {
		return c.JSON(http.StatusBadRequest, response(err.Error()))
	}

	// validate form data
	if err := data.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, response(err.Error()))
	}

	// honeypot: a legitimate front-end leaves this hidden field empty. If it's
	// filled, silently return success so bots don't learn the field is a trap,
	// but drop the submission without sending mail.
	if data.Honeypot != "" {
		s.logger.Warn().Str("form", data.FormID).Msg("honeypot triggered; dropping submission")
		return c.JSON(http.StatusOK, response("form was submitted successfully"))
	}

	// check if domain allowed (defense in depth; the Origin header is only
	// enforced by browsers, so this is not a substitute for the checks below)
	origin := c.Request().Header.Get("Origin")
	if !form.OriginDomainAllowed(origin) {
		return c.JSON(http.StatusForbidden, response("you're not allowed to send from this domain"))
	}

	// captcha: when Turnstile is configured, verify the token server-side. This
	// is the real gate against non-browser abuse, since a script can forge Origin.
	if s.mailer.TurnstileEnabled() {
		ok, err := s.mailer.VerifyTurnstile(c.Request().Context(), data.TurnstileToken, c.RealIP())
		if err != nil {
			s.logger.Error().Err(err).Str("form", data.FormID).Msg("turnstile verification failed to complete")
			return c.JSON(http.StatusBadGateway, response("couldn't verify the captcha"))
		}
		if !ok {
			return c.JSON(http.StatusForbidden, response("captcha verification failed"))
		}
	}

	// send the mail
	if err := s.mailer.Send(c.Request().Context(), data); err != nil {
		s.logger.Error().Err(err).Str("form", data.FormID).Msg("failed to send form submission email")
		return c.JSON(http.StatusInternalServerError, response("couldn't send the mail"))
	}

	return c.JSON(http.StatusOK, response("form was submitted successfully"))
}
