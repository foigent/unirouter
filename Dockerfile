FROM golang:1.25.12-alpine AS builder
WORKDIR /app
ARG RUNTIME_VERSION=local
ARG RUNTIME_COMMIT=unknown
ARG RUNTIME_TREE=unknown
ARG GOPROXY=https://proxy.golang.org,direct
ARG UNIROUTER_RELEASE_TRUST_MODE=release
ARG UNIROUTER_LICENSE_TRUST_BUNDLE_B64=
ARG UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN test -e /app/.env.example \
    && test -z "$(find /app -type f \( \
        -name '.env' -o -name '.env.*' -o \
        -name '*.pem' -o -name '*.key' -o \
        -name '*.private' -o -name '*.p12' -o -name '*.pfx' \
      \) ! -name '.env.example' -print -quit)" \
    && test -z "$(find /app -type d \( \
        -name '.secrets' -o -name 'secrets' \
      \) -print -quit)"
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    case "${UNIROUTER_RELEASE_TRUST_MODE}" in \
      release) \
        case "${RUNTIME_VERSION}" in \
          ''|local|unknown|*[!A-Za-z0-9._+-]*) echo "release builds require a safe immutable runtime version" >&2; exit 1 ;; \
        esac; \
        test "${#RUNTIME_VERSION}" -le 128 \
        || { echo "release runtime version exceeds 128 bytes" >&2; exit 1; }; \
        for trust_bundle in "${UNIROUTER_LICENSE_TRUST_BUNDLE_B64}" "${UNIROUTER_UPGRADE_TRUST_BUNDLE_B64}"; do \
          case "${trust_bundle}" in \
            ''|*[!A-Za-z0-9_-]*) echo "release trust bundles must be non-empty unpadded base64url" >&2; exit 1 ;; \
          esac; \
        done; \
        case "${RUNTIME_COMMIT}" in \
          ''|*[!0-9a-f]*) echo "release builds require a lowercase hexadecimal runtime commit" >&2; exit 1 ;; \
        esac; \
        case "${#RUNTIME_COMMIT}" in \
          40|64) ;; \
          *) echo "release builds require a complete 40- or 64-character runtime commit" >&2; exit 1 ;; \
        esac; \
        case "${RUNTIME_TREE}" in \
          ''|*[!0-9a-f]*) echo "release builds require a lowercase hexadecimal runtime tree" >&2; exit 1 ;; \
        esac; \
        case "${#RUNTIME_TREE}" in \
          40|64) ;; \
          *) echo "release builds require a complete 40- or 64-character runtime tree" >&2; exit 1 ;; \
        esac ;; \
      development) ;; \
      *) echo "UNIROUTER_RELEASE_TRUST_MODE must be release or development" >&2; exit 1 ;; \
    esac \
    && CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w \
        -X main.buildVersion=${RUNTIME_VERSION} \
        -X main.buildCommit=${RUNTIME_COMMIT} \
        -X main.buildTree=${RUNTIME_TREE} \
        -X github.com/uniroute/unirouter/internal/releasetrust.buildMode=${UNIROUTER_RELEASE_TRUST_MODE} \
        -X github.com/uniroute/unirouter/internal/releasetrust.embeddedLicenseKeyRingBase64=${UNIROUTER_LICENSE_TRUST_BUNDLE_B64} \
        -X github.com/uniroute/unirouter/internal/releasetrust.embeddedUpgradeKeyRingBase64=${UNIROUTER_UPGRADE_TRUST_BUNDLE_B64}" \
      -o /app/uniroute ./cmd/uniroute/

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/uniroute .
COPY migrations/ ./migrations/
EXPOSE 8080
CMD ["./uniroute"]
