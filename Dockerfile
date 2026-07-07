# ─── Stage 1: Frontend build ─────────────────────────────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN npm install -g pnpm && pnpm install --frozen-lockfile
COPY frontend/ .
RUN pnpm build

# ─── Stage 2: Go binary build ───────────────────────────────────────────────
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
ARG VERSION=dev
ARG COMMIT=none
ARG CHANNEL=stable
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w \
      -X github.com/stefanriegel/Universal-Token-Assessment/internal/version.Version=${VERSION} \
      -X github.com/stefanriegel/Universal-Token-Assessment/internal/version.Commit=${COMMIT} \
      -X github.com/stefanriegel/Universal-Token-Assessment/internal/version.Channel=${CHANNEL}" \
    -o universal-token-assessment .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w" \
    -o healthcheck ./cmd/healthcheck
RUN mkdir -p /tmp-empty && chmod 1777 /tmp-empty

# ─── Stage 3: Minimal scratch image ─────────────────────────────────────────
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder --chmod=1777 /tmp-empty /tmp
COPY --from=builder /app/healthcheck /healthcheck
COPY --from=builder /app/universal-token-assessment /universal-token-assessment
ENV NO_BROWSER=1
# Container mode binds all interfaces explicitly; the binary default is
# loopback-only (127.0.0.1:8080) so desktop installs are not LAN-reachable (issue #56).
ENV LISTEN_ADDR=0.0.0.0:8080
EXPOSE 8080
USER nobody
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/healthcheck"]
ENTRYPOINT ["/universal-token-assessment"]
