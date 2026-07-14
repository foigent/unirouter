FROM golang:1.25.12-alpine AS builder
WORKDIR /app
ARG RUNTIME_VERSION=local
ARG RUNTIME_COMMIT=unknown
ARG GOPROXY=https://proxy.golang.org,direct
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
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.buildVersion=${RUNTIME_VERSION} -X main.buildCommit=${RUNTIME_COMMIT}" -o /app/uniroute ./cmd/uniroute/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/uniroute .
COPY migrations/ ./migrations/
EXPOSE 8080
CMD ["./uniroute"]
