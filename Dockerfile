# syntax=docker/dockerfile:1
# Compose selects "local", which builds the frontend and Go binary from source.
# The default "release" target reuses CI's precompiled dist/<TARGETARCH>/artex.
# Both targets share the same runtime. Builders run natively and cross-compile Go.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS frontend
WORKDIR /src/web
ENV NEXT_TELEMETRY_DISABLED=1
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:static

FROM --platform=$BUILDPLATFORM golang:1.26.3-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Copy application sources explicitly, excluding host credentials and local reports.
COPY agent/ ./agent/
COPY cmd/ ./cmd/
COPY codexauth/ ./codexauth/
COPY config/ ./config/
COPY db/ ./db/
COPY enrich/ ./enrich/
COPY evidence/ ./evidence/
COPY guard/ ./guard/
COPY intercept/ ./intercept/
COPY llmpool/ ./llmpool/
COPY llmrec/ ./llmrec/
COPY mcphttp/ ./mcphttp/
COPY notify/ ./notify/
COPY report/ ./report/
COPY selfupdate/ ./selfupdate/
COPY server/ ./server/
COPY sidequestion/ ./sidequestion/
COPY traffic/ ./traffic/
COPY --from=frontend /src/web/out/ ./server/webui/dist/
ARG TARGETOS
ARG TARGETARCH
ARG ARTEX_BUILD_VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -tags embedui -trimpath \
    -ldflags "-s -w -buildid= -X main.version=${ARTEX_BUILD_VERSION}" \
    -o /out/artex ./cmd/artex

FROM python:3.12-slim-bookworm AS runtime
# Pin the CLI used for ChatGPT login refresh and model discovery. Credentials
# are imported at runtime; no auth files or tokens are build arguments.
ARG CODEX_VERSION=0.169.0
# Common tools: ripgrep, curl, vim, and frequently used reconnaissance utilities (adjust as needed).
# Install Node 20.x from NodeSource: bookworm provides Node 18, but Playwright requires >=20.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
RUN npm install -g "@openai/codex@${CODEX_VERSION}" \
    && codex --version \
    && npm cache clean --force
# Preinstall Playwright MCP and CLI globally to avoid runtime downloads through npx.
# @playwright/mcp: run the browser MCP with `npx @playwright/mcp` (globally installed; no -y/@latest needed).
# @playwright/cli: provides playwright-cli; verify availability with --help after installation.
# Then install playwright for browser management and use --with-deps to preinstall Chromium and system dependencies.
# This makes the container MCP/CLI ready on first launch without downloading browsers at runtime.
RUN npm install -g @playwright/mcp@latest @playwright/cli@latest playwright@latest \
    && playwright-cli --help \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Supervisor script: restart according to the exit code; one-click updates use this script to complete replacement.
# It also forwards SIGTERM to artex because docker stop sends signals only to PID 1.
# Without forwarding, artex cannot shut down gracefully and is killed with SIGKILL after 10 seconds.
COPY start.sh /app/start.sh
COPY scripts/docker-entrypoint.py /app/docker-entrypoint.py
ENV CODEX_HOME=/app/data/codex
RUN chmod +x /app/start.sh
COPY skills/ /app/skills/
# Persistent data/ directory (SQLite and jwt.key)
VOLUME ["/app/data"]
EXPOSE 8787 8788
ENTRYPOINT ["python3", "/app/docker-entrypoint.py"]
CMD ["-addr", ":8787", "-proxy", ":8788"]

FROM runtime AS local
COPY --from=backend --chmod=755 /out/artex /app/artex

# Keep the release workflow's precompiled-binary build as the default target.
FROM runtime AS release
ARG TARGETARCH
COPY --chmod=755 dist/${TARGETARCH}/artex /app/artex
