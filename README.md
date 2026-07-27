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
```


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
2. **Honeypot** — every submission may include a hidden `_gotcha` field.
   Legitimate front-ends keep it empty; bots that fill every field trip it.
   A tripped honeypot returns a normal success response but silently drops the
   message (so bots don't learn the field is a trap). No configuration needed.
3. **Cloudflare Turnstile** — the real gate against non-browser abuse. The
   `allowed_domains` / `Origin` check alone is **not** sufficient, because any
   script can forge the `Origin` header; only a client-side challenge like
   Turnstile actually stops automated submissions.

### Enabling Turnstile

1. Create a Turnstile widget in the Cloudflare dashboard and copy the **secret
   key** into `global.turnstile.secret`. When the secret is empty, Turnstile
   verification is disabled and only the honeypot + rate limiting apply.
2. Add the Turnstile widget to your form's front-end and submit the resulting
   token as `cf-turnstile-response` (the widget's default field name):

```html
<form ...>
    <!-- your fields -->

    <!-- honeypot: keep it visually hidden -->
    <input type="text" name="_gotcha" style="display:none" tabindex="-1" autocomplete="off">

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



## Examples


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



### MailBear with jQuery


```html
<form id="contact-form" class="pure-form">
            
    <div class="meta">
        <input type="text" name="name" placeholder="Naam">
        
        <input type="email" name="email" placeholder="Email">

        <input type="text" name="subject" placeholder="Subject">
    </div><!-- .meta -->
    
    
    <textarea name="content" placeholder="Your message" rows="7"></textarea>
    
    <button type="submit" class="">
        <i class="fa fa-send-o"></i> Send
    </button>

    
    <div class="overlay">
        <div> 
        </div>
    </div><!-- .overlay -->
    
    
</form>



<script>
    $( document ).ready(function() {    
        
        var $contactForm = $('#contact-form');
        $contactForm.submit(function(e) {
            e.preventDefault();
            $.ajax({
                url: 'https://mailbear.yourdomain.com/api/v1/form/10810dce-1074-4988-a8f5-4c538a749a95',
                method: 'POST',
                data: $(this).serialize(),
                dataType: 'json',
                beforeSend: function() {
                    $contactForm.find('.overlay div').html('<div class="alert alert--loading"><i class="fa fa-circle-o-notch fa-spin"></i> &nbsp; Sending message...</div>');
                    $contactForm.find('.overlay').fadeIn();
                },
                success: function(data) {
                    $contactForm.find('.alert--loading').hide();
                    $contactForm.find('.overlay div').html('<div class="alert alert--success"><i class="fa fa-check"></i> &nbsp; Your message was sent successfully!</div>');
                    $contactForm.find('.overlay').fadeIn();
                },
                error: function(err) {
                    $contactForm.find('.alert--loading').hide();
                    $contactForm.find('.overlay div').html('<div class="alert alert--error"><i class="fa fa-warning"></i> &nbsp; Ooops, something went wrong.</div>');
                    $contactForm.find('.overlay').fadeIn();
                }
            });
        });
        $contactForm.find('.overlay').click(function(e) {
            $(this).fadeOut(); 
        });                
    });
</script>
```


## Metrics

Prometheus metrics can be found on `:9090/metrics` by default. To get statistics of submissions per form use this metric: `mailbear_form_submissions_total{form="some-form-name"}`.

A Grafana dashboard for these metrics is available here: [./grafana/dashboard.json](grafana/dashboard.json)

[![](grafana/dashboard_small.png)](grafana/dashboard.png)


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