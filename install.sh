#!/usr/bin/env bash
# ARTEX installer: 1) Full Docker deployment  2) Build and run locally
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }
rand(){ head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24; }

# -- Detect Docker / install automatically --
ensure_docker(){
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    ok "docker and docker compose are available"; return
  fi
  warn "docker / docker compose was not found"
  case "$(uname -s)" in
    Linux)
      if [ "$(ask 'Install Docker automatically? (y/n)' y)" = y ]; then
        curl -fsSL https://get.docker.com | sh
        sudo usermod -aG docker "$USER" || true
        ok "Docker installation complete (log in again for group changes to allow use without sudo)"
      else
        die "Install docker manually and run this script again"
      fi ;;
    Darwin) die "On macOS, install Docker Desktop: https://www.docker.com/products/docker-desktop/" ;;
    *)      die "Install docker manually and run this script again" ;;
  esac
}

# -- 1) Full Docker deployment --
install_docker(){
  ensure_docker
  if [ ! -f .env ]; then
    cp .env.example .env 2>/dev/null || true
    local pw key
    pw="$(ask 'Postgres password (press Enter to generate a random value)' "$(rand)")"
    key="$(ask 'ANTHROPIC_API_KEY (optional; configure later in the UI)' '')"
    sed -i.bak "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${pw}|" .env
    sed -i.bak "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=${key}|" .env
    rm -f .env.bak
    ok "Created .env (POSTGRES_PASSWORD configured)"
  else
    info "Using the existing .env file"
  fi
  info "Pulling images and starting services..."
  docker compose pull || true
  if ! docker compose up -d; then
    warn "Could not pull or start ghcr.io/jiwoochris/artex-global (check that ARTEX_TAG in .env is a published tag and the network is available)"
    warn "Run this script again and select \"2) Run locally (build with Go)\", or follow the single-binary source build instructions in README"
    die "Docker Compose (full Docker deployment) failed to start"
  fi
  ok "Startup complete -> http://localhost:8787"
  info "On your first visit, enter the \"setup token\" at /setup to create the administrator password"
  info "Find the setup token: docker compose logs artex | grep -F '[auth]'   (published on 127.0.0.1 by default; change ARTEX_BIND in .env to override)"
  info "View logs: docker compose logs -f artex"
}

# -- 2) Build and run locally --
install_local(){
  echo "Database setup:"
  echo "  1) Connect to an existing PostgreSQL instance"
  echo "  2) Start a PostgreSQL container (requires docker)"
  case "$(ask 'Select' 1)" in
    2)
      ensure_docker
      local pw; pw="$(ask 'Postgres password (press Enter for a random value)' "$(rand)")"
      docker run -d --name artex-pg -p 5432:5432 \
        -e POSTGRES_USER=artex -e POSTGRES_PASSWORD="$pw" -e POSTGRES_DB=artex \
        -v artex-pg:/var/lib/postgresql/data postgres:16-alpine
      DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=artex DB_PASS="$pw" DB_NAME=artex DB_SSL=disable ;;
    *)
      DB_HOST="$(ask 'Database host' 127.0.0.1)"
      DB_PORT="$(ask 'Port' 5432)"
      DB_USER="$(ask 'Username' artex)"
      DB_PASS="$(ask 'Password' '')"
      DB_NAME="$(ask 'Database name' artex)"
      DB_SSL="$(ask 'sslmode (disable/require)' disable)" ;;
  esac

  # Create config.json
  cat > config.json <<JSON
{
  "database": {
    "host": "${DB_HOST}",
    "port": ${DB_PORT},
    "user": "${DB_USER}",
    "password": "${DB_PASS}",
    "dbname": "${DB_NAME}",
    "sslmode": "${DB_SSL}"
  }
}
JSON
  ok "Created config.json"

  # Check the Go environment
  command -v go >/dev/null 2>&1 || die "Go was not found. Install Go (>=1.26) first: https://go.dev/dl/"
  ok "Go: $(go version)"

  # Embedding the frontend requires Node to generate static assets
  if command -v npm >/dev/null 2>&1; then
    info "Building static frontend assets..."
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && mkdir -p server/webui && cp -r web/out server/webui/dist
    info "Compiling a single binary with the embedded frontend..."
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm was not found: building only the backend without an embedded frontend (run the frontend separately with npm run dev)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "Build complete -> ./artex"

  info "Starting... (Ctrl-C to stop). The default bind address is 127.0.0.1:8787. On your first visit, enter the [auth] setup token from the logs below at /setup"
  ./artex
}

echo "=============================="
echo "  ARTEX installation"
echo "  1) Full Docker deployment"
echo "  2) Run locally (build with Go)"
echo "=============================="
case "$(ask 'Select' 1)" in
  1) install_docker ;;
  2) install_local ;;
  *) die "Invalid selection" ;;
esac
