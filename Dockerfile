FROM golang:1.26-alpine3.23 AS build

RUN apk add --no-cache make \
    && apk cache clean

WORKDIR /src

# Download dependencies first so this layer caches until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Then copy source and build via the Makefile, so build flags (ldflags, trimpath,
# version stamping) live in one place shared with local/CI builds.
COPY . .
ARG VERSION=DEV
RUN make prod VERSION=$VERSION

FROM alpine:3.24

WORKDIR /mailbear

# Copy executable
COPY --from=build /src/bin/mailbear /bin/mailbear

# Copy sample config as a default
COPY config_sample.yml /mailbear/config.yml

EXPOSE 1234
EXPOSE 9090
VOLUME ["/mailbear"]

# Operational settings (SMTP, Turnstile, addresses) are passed at runtime via env
# vars or flags; only the forms config lives in the mounted /mailbear volume.
ENTRYPOINT ["/bin/mailbear"]
CMD ["serve"]
