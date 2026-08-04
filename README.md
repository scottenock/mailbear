# 🐻 MailBear: Forms Backend

[![Test](https://github.com/laputalabs/mailbear/actions/workflows/test.yml/badge.svg)](https://github.com/laputalabs/mailbear/actions/workflows/test.yml)
[![Lint](https://github.com/laputalabs/mailbear/actions/workflows/lint.yml/badge.svg)](https://github.com/laputalabs/mailbear/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/laputalabs/mailbear)](https://goreportcard.com/report/github.com/laputalabs/mailbear)


MailBear is an open source, self hosted forms backend.
Just do a post request to the API with some form data, and MailBear will make sure the submission is sent to you via mail!

MailBear will always hide the email address of the recepient, since the forms are accessed by a unique key.


## Run with Docker

You can easily run MailBear with Docker. Copy `config_sample.yml` to `config.yml`
(it holds only your forms), then pass the operational settings as environment
variables:

    docker run \
      -v $(PWD)/config.yml:/mailbear/config.yml \
      -e SMTP_HOST=smtp.example.com \
      -e SMTP_FROM_EMAIL=no-reply@example.com \
      -p 1234:1234 \
      ghcr.io/laputalabs/mailbear:latest

A [docker-compose.yml](./docker-compose.yml) file is provided with the full set of
environment variables.


## Deploying behind Caddy

In production, terminate TLS with a reverse proxy and don't expose MailBear's
ports publicly. [Caddy](https://caddyserver.com) works out of the box — it
fetches a certificate automatically and appends `X-Forwarded-For`, which MailBear
uses to recover the real client IP for rate limiting (see
[Rate Limiting & Reverse Proxies](#rate-limiting--reverse-proxies)).

`Caddyfile`:

```caddy
forms.example.com {
    reverse_proxy mailbear:1234
}
```

Use `mailbear:1234` when Caddy shares a Docker network with MailBear (the compose
service name); use `localhost:1234` if Caddy runs directly on the host.

Crucially, **only Caddy should be publicly reachable.** Run them together and
give MailBear no published ports — Caddy reaches it over the internal network:

```yaml
services:
  caddy:
    image: caddy:2
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy_data:/data
    depends_on:
      - mailbear

  mailbear:
    image: ghcr.io/laputalabs/mailbear:v0.0.2
    command: serve
    # No `ports:` — MailBear is reachable only to Caddy, as "mailbear:1234".
    environment:
      SMTP_HOST: smtp.example.com
      SMTP_FROM_EMAIL: no-reply@example.com
      # ... other settings
    volumes:
      - ./config.yml:/mailbear/config.yml

volumes:
  caddy_data:
```

If MailBear published `1234` to the host, clients could hit it directly —
bypassing TLS and forging `X-Forwarded-For` to defeat the rate limiter. Keep the
metrics port (`:9090`) internal as well; don't proxy it publicly.


## Run in Development

Copy `config_sample.yml` to `config.yml`, then run the `serve` command:

    SMTP_HOST=smtp.example.com SMTP_FROM_EMAIL=no-reply@example.com \
      go run ./cmd/mailbear serve --config config.yml

Common tasks are wrapped in the `Makefile` (run `make help` for the full list):

    make setup   # install dev tools (gofumpt, golangci-lint, govulncheck)
    make dev     # build ./bin/mailbear
    make test    # run tests with the race detector
    make lint    # run golangci-lint
    make docker  # build the Docker image

MailBear shuts down gracefully on `SIGINT`/`SIGTERM`, draining in-flight requests.

To check a config without starting the server (handy in CI or before a deploy),
use `validate` — it runs the same checks the server does at startup (parses the
forms config, loads and parses templates, compiles each form's subject, and
requires SMTP settings when a form sends email) and exits non-zero on the first
problem:

    mailbear validate --config config.yml --templatesDir templates/


## Configuration

MailBear is configured in two places: **operational settings** via flags or
environment variables, and the **forms list** via a YAML file.

### Operational settings (flags / env)

Every flag has an environment-variable equivalent. Run `mailbear serve --help`
for the authoritative list.

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--config` | `CONFIG_FILE` | `config.yml` | Path to the forms config file |
| `--httpAddress` | `HTTP_ADDRESS` | `:1234` | API server listen address |
| `--metricsAddress` | `METRICS_ADDRESS` | `:9090` | Prometheus metrics listen address |
| `--rateLimit` | `RATE_LIMIT` | `5` | Max submissions per client IP per minute |
| `--smtpHost` | `SMTP_HOST` | — (required) | SMTP server hostname |
| `--smtpPort` | `SMTP_PORT` | `587` | SMTP server port |
| `--smtpUser` | `SMTP_USER` | — | SMTP username |
| `--smtpPassword` | `SMTP_PASSWORD` | — | SMTP password |
| `--smtpDisableTLS` | `SMTP_DISABLE_TLS` | `false` | Disable STARTTLS |
| `--smtpFromEmail` | `SMTP_FROM_EMAIL` | — (required) | From address for outgoing mail |
| `--smtpFromName` | `SMTP_FROM_NAME` | `MailBear` | From name for outgoing mail |
| `--turnstileSecret` | `TURNSTILE_SECRET` | — | Cloudflare Turnstile secret key (empty disables captcha) |
| `--honeypotField` | `HONEYPOT_FIELD` | `verify` | Name of the hidden honeypot form field |
| `--auditLog` | `AUDIT_LOG` | — | Path to a JSONL submission audit log (empty disables it) |
| `--auditLogMaxSizeMB` | `AUDIT_LOG_MAX_SIZE_MB` | `100` | Rotate the audit log once it exceeds this size (MB) |
| `--auditLogMaxBackups` | `AUDIT_LOG_MAX_BACKUPS` | `10` | Rotated audit-log files to retain (0 keeps all) |
| `--auditLogMaxAgeDays` | `AUDIT_LOG_MAX_AGE_DAYS` | `90` | Maximum age of rotated audit-log files in days (0 = no limit) |
| `--auditLogCompress` | `AUDIT_LOG_COMPRESS` | `true` | gzip rotated audit-log files |
| `--logLevel` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--prettyLog` | `PRETTY_LOG` | `true` | Pretty-print logs to the console |

### Forms (`config.yml`)

Define as many forms as you want:

```yaml
forms:
    some-form-name:
        key: some-random-key
        allowed_domains:
            - localhost:8080
            - example.com
        to_email:
            - recipient@example.com
        # Optional: display name on this form's outgoing mail (falls back to
        # SMTP_FROM_NAME). Cosmetic only — the from address is unchanged, so it
        # has no deliverability impact.
        from_name: "Acme Contact Form"
```

Each form also supports several optional fields covered in their own sections
below: `template` / `subject` ([Custom Email Templates](#custom-email-templates)),
`webhook_url` ([Webhook Forwarding](#webhook-forwarding)), `redirect_url` /
`error_redirect_url` ([Redirect After Submit](#redirect-after-submit-no-javascript-forms)),
and `autoresponder` ([Autoresponder](#autoresponder)). A full annotated example
is in [`examples/config.example.yml`](examples/config.example.yml).


## Rate Limiting & Reverse Proxies

MailBear rate-limits submissions per client IP address. To determine the client
IP it trusts the `X-Forwarded-For` header, **but only when the request arrives
from a loopback, link-local, or private-network address** (i.e. a reverse proxy
running on the same host or private network). Requests from any other source
have their `X-Forwarded-For` header ignored and are rate-limited by their real
connection IP. This prevents a client from spoofing the header to bypass the
rate limiter.

> ⚠️ **This protection depends on your reverse proxy _appending_ the real
> client IP to `X-Forwarded-For`, not blindly passing through whatever the
> client sent.** If your proxy forwards the client-supplied header verbatim, a
> client can still spoof its IP and evade rate limiting.
>
> - **Caddy** (`reverse_proxy`), **Traefik**, and **nginx** (using
>   `$proxy_add_x_forwarded_for`) all append correctly by default — no action
>   needed.
> - Also make sure MailBear itself is **not** reachable directly on a public
>   interface, bypassing the proxy. Bind it to localhost or a private network
>   and let only the proxy reach it.

If you run MailBear **without** a reverse proxy (exposed directly to clients),
no configuration is needed: the connection IP is used directly and cannot be
spoofed.


## Spam & Abuse Protection

The form endpoint is public, so it needs protection against bots and abuse.
MailBear applies these defences, from cheapest to strongest:

1. **Rate limiting** — per client IP (see the section above).
2. **Honeypot** — every submission may include a hidden decoy field (named
   `verify` by default). Legitimate front-ends keep it empty; bots that fill
   every field trip it. A tripped honeypot returns a normal success response but
   silently drops the message (so bots don't learn the field is a trap). No
   configuration needed, but you can rename the field with `--honeypotField` /
   `HONEYPOT_FIELD`; pick a non-obvious name (avoid the `_gotcha` that other form
   backends use, which bots recognise and skip). Update your form's hidden field
   to match.
3. **Cloudflare Turnstile** — the real gate against non-browser abuse. The
   `allowed_domains` / `Origin` check alone is **not** sufficient, because any
   script can forge the `Origin` header; only a client-side challenge like
   Turnstile actually stops automated submissions.

### Enabling Turnstile

1. Create a Turnstile widget in the Cloudflare dashboard and pass the **secret
   key** via `--turnstileSecret` / `TURNSTILE_SECRET`. When it is empty, Turnstile
   verification is disabled and only the honeypot + rate limiting apply.
2. Add the Turnstile widget to your form's front-end and submit the resulting
   token as `cf-turnstile-response` (the widget's default field name):

```html
<form ...>
    <!-- your fields -->

    <!-- honeypot: keep it visually hidden (name must match --honeypotField) -->
    <input type="text" name="verify" style="display:none" tabindex="-1" autocomplete="off">

    <!-- Turnstile widget (renders the cf-turnstile-response field) -->
    <div class="cf-turnstile" data-sitekey="YOUR_SITE_KEY"></div>
</form>
<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>
```

MailBear verifies the token server-side against Cloudflare's `siteverify`
endpoint on every submission and rejects any that fail.


## Usage

Once MailBear is running you can send requests with form data in the JSON body:

```bash
curl \
    -X POST \
    http://localhost:1234/api/v1/form/some-random-key \
    -H 'Content-Type: application/json' \
    -H 'Origin: http://localhost:8080' \
    -d '{"name":"Joe","email":"joe@example.com", "subject": "Some subject", "content": "Maecenas faucibus mollis interdum. Sed posuere consectetur est at lobortis."}'
```



## Custom Email Templates

By default MailBear sends a built-in email. You can give each form its own email
by pointing `--templatesDir` / `TEMPLATES_DIR` at a directory of template files:

```
templates/
    default.html      # overrides the built-in default (optional)
    contact.html      # used by forms with `template: contact`
    contact.txt       # optional plain-text part
```

- A template is a pair of files named `<name>.html` and `<name>.txt`. The `.html`
  part is **required**; the `.txt` part is **optional**.
- When both exist, the email is sent as **multipart/alternative** (plain-text +
  HTML). When only `.html` exists, it is HTML-only.
- A form selects its template with the `template:` field in `config.yml` (the
  value is the file `<name>`). Forms with no `template` use the built-in
  `default`, which you can override by placing your own `default.html` in the
  directory.
- The subject line is set per form with the optional `subject:` field, rendered as
  a Go `text/template`.

Templates are Go templates. HTML templates use
[`html/template`](https://pkg.go.dev/html/template) (which auto-escapes all
values, so user input can't inject markup); text templates use
[`text/template`](https://pkg.go.dev/text/template). Available variables:

| Variable | Description |
|----------|-------------|
| `{{ .Name }}` | Submitter's name |
| `{{ .Email }}` | Submitter's email |
| `{{ .Subject }}` | Submitter's subject |
| `{{ .Content }}` | Submitted message |
| `{{ .FormName }}` | The form's config key (human-readable name) |

A helper `{{ nl2br .Content }}` is available in HTML templates to render newlines
as `<br>` (it escapes the content first, so it stays injection-safe).

Example `contact.html`:

```html
<p>New message from <b>{{ .Name }}</b> ({{ .Email }}):</p>
<p>{{ nl2br .Content }}</p>
```

Templates are loaded and validated at startup, so a missing template file or a
syntax error fails fast rather than at send time.


## Redirect After Submit (no-JavaScript forms)

A plain HTML form (no JavaScript) that posts straight to MailBear would otherwise
land the visitor on a page showing the raw JSON response. Set `redirect_url` on
the form and, for browser form posts, MailBear replies with a `303 See Other` to
that page on success instead. Add `error_redirect_url` to send failures (bad
input, spam, delivery errors) to an error page.

```yaml
forms:
    contact:
        key: contact-key
        allowed_domains: [example.com]
        to_email: [me@example.com]
        redirect_url: https://example.com/thanks
        error_redirect_url: https://example.com/oops   # optional
```

```html
<form action="https://mailbear.example.com/api/v1/form/contact-key" method="POST">
    <input type="email" name="email" required>
    <input type="text" name="subject" required>
    <textarea name="content" required></textarea>
    <button type="submit">Send</button>
</form>
```

Redirects apply only to browser form posts (`application/x-www-form-urlencoded` /
`multipart/form-data`). JSON/AJAX clients always receive a JSON response, so the
existing integrations are unaffected. When `error_redirect_url` is not set,
failures fall back to a JSON body. Both URLs are operator config, so there is no
open-redirect risk from client input.


## Autoresponder

A form can send a confirmation email back to the **submitter** after a successful
submission. Add an `autoresponder` block referencing its own template (a
`<name>.html` / `<name>.txt` pair in the `--templatesDir`); the `subject` is
optional and rendered as a `text/template`:

```yaml
forms:
    contact:
        key: contact-key
        allowed_domains: [example.com]
        to_email: [me@example.com]
        autoresponder:
            template: contact-ack
            subject: "Thanks for contacting us, {{.Name}}"
```

The confirmation uses the same template variables as any other template
(`{{.Name}}`, `{{.Content}}`, …), goes out from the configured SMTP `from`
address, and sets `Reply-To` to the form's first recipient so replies reach you.
It is **best-effort**: the owner notification is sent first, and if the
confirmation later fails to send, the request still succeeds (so the submitter is
never prompted to resubmit and re-notify you). Outcomes are counted in
`mailbear_autoresponder_deliveries_total`.

> An autoresponder emails whatever address the submitter typed, so it can be
> abused to send mail to arbitrary addresses. It only fires **after** the
> honeypot, Origin, Turnstile, and rate-limit checks pass — enabling
> [Turnstile](#spam--abuse-protection) is strongly recommended when using it.


## Webhook Forwarding

Each form can POST its submissions as JSON to a URL via the `webhook_url` field —
useful for Slack, Discord, Zapier, n8n, or any HTTP endpoint. A form needs **at
least one** of `to_email` or `webhook_url`; set only `webhook_url` for a form that
delivers by webhook and sends no email at all (in which case no SMTP config is
required).

```yaml
forms:
    contact:                       # email + webhook
        key: contact-key
        allowed_domains: [example.com]
        to_email: [me@example.com]
        webhook_url: https://hooks.example.com/contact
    alerts:                        # webhook only, no email
        key: alerts-key
        allowed_domains: [example.com]
        webhook_url: https://n8n.example.com/webhook/abc
```

The payload is a JSON object:

```json
{"form":"contact","name":"Joe","email":"joe@example.com","subject":"Hi","content":"the message"}
```

Delivery semantics:

- **Email is the primary channel.** When a form has `to_email`, a delivery failure
  fails the request (the submitter sees an error and can retry).
- **A webhook is best-effort when email is also configured**: if the email is sent
  but the webhook POST fails, the request still succeeds and the failure is logged
  (and counted in `mailbear_webhook_deliveries_total{outcome="failure"}`).
- **When the webhook is the only channel**, a non-2xx response or a connection
  error fails the request.

> The `webhook_url` is operator configuration (from `config.yml`), never user
> input, so it is trusted and not SSRF-filtered. Only point it at endpoints you
> control or trust.


## Examples


Ready-to-use integration snippets live in the [`examples/`](./examples) directory:

- [`plain_html.html`](examples/plain_html.html) — a no-JavaScript HTML form (uses `redirect_url` for the result page).
- [`fetch.html`](examples/fetch.html) — vanilla JS `fetch()` with a honeypot and a Cloudflare Turnstile widget.
- [`react_example.jsx`](examples/react_example.jsx) — a React (hooks) form component.
- [`vuejs_example.vue`](examples/vuejs_example.vue) — a Vue single-file component (shown below).


### MailBear with VueJS

```html
<template>
    <div id="contact">
  
      <div class="form" >

  
          <form @submit.prevent="submit">
  
              <div class="form-overlay" v-if="loading">
                  <font-awesome-icon icon="circle-notch" spin />
              </div><!-- form-overlay -->
  
              <div>
                  <div class="status" v-if="status !== ''">
                      <span v-if="status === 'success'">Your email has successfully been sent.</span>
                      <span v-if="status === 'error'">Something went wrong while sending your email.</span>
                  </div>
              </div>
  
              <div>
                  <input type="text" name="name" v-model="form_data.name" placeholder="Name or Company" required />
              </div>
  
              <div>
                  <input type="email" name="email" v-model="form_data.email" placeholder="Email" required />
              </div>
  
              <div>
                  <input type="text" name="subject" v-model="form_data.subject" placeholder="Subject" required />
              </div>
  
  
              <div>
                  <textarea type="text" name="content" v-model="form_data.content" placeholder="Message" rows="6" required />
              </div>
  
  
              <div>
                  <button type="submit">Send</button>
              </div>
          
        </form>
  
      </div>
  
    </div><!-- contact -->
</template>
  
<script>

import config from '../config'


export default {
    name: 'Contact',
    components: {
    },
    data: function() {
        return {
            contact_text: "",
            form_data: {
                name: "",
                email: "",
                subject: "",
                content: ""
            },
            status: "",
            loading: false
        }
    },
    created() {
    },
    mounted() {
    },
    methods: {
        clearForm: function() {
            this.form_data.name    = "";
            this.form_data.email   = "";
            this.form_data.subject = "";
            this.form_data.content = "";
        },
        submit: function(e) {
            e.preventDefault();

            var self = this
            self.loading = true
            
            this.axios.post(config.MAILBEAR_URL + `/api/v1/form/10810dce-1074-4988-a8f5-4c538a749a95`, this.form_data)
            .then(response => {
                self.status = "success"
                self.clearForm()

                return response
            })
            .catch(error => {
                self.status = "error"
                console.log(error)
            })
            .then(function () {
                // always executed
                self.loading = false
            })
        }
    }
}
</script>

<style lang="scss">
/*
 * Style was left out of this example. 
 * Go find it in ./examples/vuejs_example.vue
 */
</style>
```



## Metrics

Prometheus metrics are served on `:9090/metrics` by default. The exposed metrics:

| Metric | Labels | Description |
|--------|--------|-------------|
| `mailbear_form_requests_total` | `form`, `result` | Every submission request by outcome. `result` is one of `success`, `honeypot`, `invalid`, `forbidden_origin`, `captcha_failed`, `captcha_error`, `send_error`, `not_found`. |
| `mailbear_form_submissions_total` | `form` | Submissions successfully delivered (email sent and/or webhook accepted). |
| `mailbear_webhook_deliveries_total` | `form`, `outcome` | Webhook POSTs, `outcome` = `success` / `failure`. |
| `mailbear_autoresponder_deliveries_total` | `form`, `outcome` | Autoresponder confirmation emails, `outcome` = `success` / `failure`. |
| `mailbear_rate_limited_total` | — | Requests rejected by the rate limiter. |

For example, a spam/rejection breakdown per form comes from
`mailbear_form_requests_total`, while `mailbear_form_submissions_total` counts
what actually got delivered.

The endpoint uses the standard Prometheus exposition format, so any
Prometheus-compatible stack can scrape it — build whatever dashboards or alerts
you like on top of these metrics in the tool of your choice.


## Health Checks

MailBear exposes two unauthenticated probe endpoints on the API port for load
balancers and Kubernetes liveness/readiness probes:

- `GET /healthz` — liveness
- `GET /readyz` — readiness

Both return `200 OK` when the server is serving. They bypass the rate limiter and
request logging, so probing them frequently is safe.


## Audit Log

Set `--auditLog` / `AUDIT_LOG` to a file path and MailBear appends every accepted
submission — with its delivery outcome — as a line of JSON
([JSON Lines](https://jsonlines.org)). It's off by default. The record is written
regardless of whether delivery succeeded, so nothing is lost if your mail server
is down: you can inspect or replay the file later.

```
{"ts":"2026-07-28T10:15:03Z","form":"contact","name":"Ada","email":"ada@example.com","subject":"Question","content":"How much?","delivered":true}
{"ts":"2026-07-28T10:16:41Z","form":"contact","name":"Grace","email":"grace@example.com","subject":"Hi","content":"hello","delivered":false}
```

Only submissions that pass validation and the anti-abuse checks are logged (spam
and honeypot hits are not). Query it with standard tools, e.g.
`jq 'select(.delivered==false)' audit.jsonl` to find deliveries that failed.

The log is **rotated automatically**: once the active file passes
`--auditLogMaxSizeMB` (default 100 MB) it's rolled over, older files beyond
`--auditLogMaxBackups` (default 10) or `--auditLogMaxAgeDays` (default 90) are
deleted, and rotated files are gzipped unless `--auditLogCompress=false`. Tune
these to match your retention needs.

> ⚠️ **This stores personal data** (names, emails, message contents) on disk.
> Put the file on a persistent, access-controlled volume, and set the retention
> knobs above to whatever your data-protection obligations require (the defaults
> keep roughly 90 days / 10 rotations).


## Acknowledgements

* [github.com/spf13/cobra](https://github.com/spf13/cobra)
* [github.com/go-chi/chi](https://github.com/go-chi/chi)
* [github.com/go-chi/httprate](https://github.com/go-chi/httprate)
* [github.com/rs/zerolog](https://github.com/rs/zerolog)
* [github.com/go-yaml/yaml](https://github.com/go-yaml/yaml)
* [github.com/go-mail/mail](https://github.com/go-mail/mail)
* [github.com/badoux/checkmail](https://github.com/badoux/checkmail)
* [github.com/prometheus/client_golang](https://github.com/prometheus/client_golang)


## Credits

Forked from [MailBear](https://github.com/DenBeke/mailbear) by [Mathias Beke](https://denbeke.be).