package mailbear

import (
	"net/http"

	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

func (m *MailBear) handleForm(c echo.Context) error {
	formID := c.Param("id")

	// check if form exists
	if !m.formExists(formID) {
		return c.JSON(http.StatusNotFound, mailbearRespone("the given form does not exist"))
	}

	// get form data from body
	data := &FormSubmission{FormID: formID}
	if err := c.Bind(data); err != nil {
		return c.JSON(http.StatusBadRequest, mailbearRespone(err.Error()))
	}

	// validate form data
	if err := data.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, mailbearRespone(err.Error()))
	}

	// honeypot: a legitimate front-end leaves this hidden field empty. If it's
	// filled, silently return success so bots don't learn the field is a trap,
	// but drop the submission without sending mail.
	if data.Honeypot != "" {
		log.WithField("form", data.FormID).Warn("honeypot triggered; dropping submission")
		return c.JSON(http.StatusOK, mailbearRespone("form was submitted successfully"))
	}

	// check if domain allowed (defense in depth; the Origin header is only
	// enforced by browsers, so this is not a substitute for the checks below)
	origin := c.Request().Header.Get("Origin")
	form := m.getFormByID(data.FormID)
	if !form.OriginDomainAllowed(origin) {
		return c.JSON(http.StatusForbidden, mailbearRespone("you're not allowed to send from this domain"))
	}

	// captcha: when Turnstile is configured, verify the token server-side. This
	// is the real gate against non-browser abuse, since a script can forge Origin.
	if m.config.TurnstileEnabled() {
		ok, err := m.verifyTurnstile(c.Request().Context(), data.TurnstileToken, c.RealIP())
		if err != nil {
			log.WithFields(log.Fields{
				"form":  data.FormID,
				"error": err,
			}).Error("turnstile verification failed to complete")
			return c.JSON(http.StatusBadGateway, mailbearRespone("couldn't verify the captcha"))
		}
		if !ok {
			return c.JSON(http.StatusForbidden, mailbearRespone("captcha verification failed"))
		}
	}

	// send the mail
	err := m.SendMail(data)
	if err != nil {
		log.WithFields(log.Fields{
			"form":  data.FormID,
			"error": err,
		}).Error("failed to send form submission email")
		return c.JSON(http.StatusInternalServerError, mailbearRespone("couldn't send the mail"))
	}

	return c.JSON(http.StatusOK, mailbearRespone("form was submitted successfully"))
}
