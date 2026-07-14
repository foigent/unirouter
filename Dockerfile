FROM golang:1.25.12-alpine AS builder
WORKDIR /app
ARG RUNTIME_VERSION=local
ARG RUNTIME_COMMIT=unknown
ARG GOPROXY=https://proxy.golang.org,direct
ARG UNIROUTER_RELEASE_TRUST_MODE=release
ARG UNIROUTER_LICENSE_TRUST_BUNDLE_B64=
ARG UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN test -e /app/.env.example \
    && test -z "$(find /app -type f \( \
        -name '.env' -o -name '.env.*' -o \
        -name '*.pem' -o -name '*.key' -o \
        -name '*.p12' -o -name '*.pfx' \
      \) ! -name '.env.example' -print -quit)" \
    && test -z "$(find /app -type d \( \
        -name '.secrets' -o -name 'secrets' \
      \) -print -quit)"
RUN case "${UNIROUTER_RELEASE_TRUST_MODE}" in \
      release) \
        test -n "${UNIROUTER_LICENSE_TRUST_BUNDLE_B64}" \
        && test -n "${UNIROUTER_UPGRADE_TRUST_BUNDLE_B64}" ;; \
      development) ;; \
      *) echo "UNIROUTER_RELEASE_TRUST_MODE must be release or development" >&2; exit 1 ;; \
    esac \
    && CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w \
        -X main.buildVersion=${RUNTIME_VERSION} \
        -X main.buildCommit=${RUNTIME_COMMIT} \
        -X github.com/uniroute/unirouter/internal/releasetrust.buildMode=${UNIROUTER_RELEASE_TRUST_MODE} \
        -X github.com/uniroute/unirouter/internal/releasetrust.embeddedLicenseKeyRingBase64=${UNIROUTER_LICENSE_TRUST_BUNDLE_B64} \
        -X github.com/uniroute/unirouter/internal/releasetrust.embeddedUpgradeKeyRingBase64=${UNIROUTER_UPGRADE_TRUST_BUNDLE_B64}" \
      -o /app/uniroute ./cmd/uniroute/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/uniroute .
COPY migrations/ ./migrations/
EXPOSE 8080
CMD ["./uniroute"]
