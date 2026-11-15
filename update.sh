#!/usr/bin/env bash
# ARTEX updater: 1) Docker update (pull and recreate)  2) Local update (rebuild the binary)
# Companion to install.sh: install handles initial setup, while update upgrades to a new version.
# No manual database migration is needed: artex reruns schema.sql idempotently at startup (including ADD COLUMN/CREATE
# INDEX IF NOT EXISTS), so restarting applies migrations. Data (the pgdata volume, ./data, ./skills) is preserved.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }

# -- Optional: sync the repository to update Compose files, scripts, and local build sources --
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { warn "Not a git working copy; skipping git pull"; return; }
  [ "$(ask 'Fetch the latest code (git pull --ff-only)? (y/n)' y)" = y ] || return
  if ! git pull --ff-only; then
    warn "git pull could not fast-forward (local changes or diverged branches). Resolve this manually and retry. Continuing with the current code"
  fi
}

# -- 1) Docker update --
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
    || die "docker / docker compose was not found. Install and deploy with ./install.sh first"
  [ -f .env ] || die ".env was not found. Complete the initial deployment with ./install.sh first"

  # Optional: upgrade to a specific image tag (leave blank to use ARTEX_TAG in .env, defaulting to latest)
  local tag; tag="$(ask 'Target image tag (press Enter to use .env / latest)' '')"
  if [ -n "$tag" ]; then
    if grep -q '^ARTEX_TAG=' .env; then
      sed -i.bak "s|^ARTEX_TAG=.*|ARTEX_TAG=${tag}|" .env && rm -f .env.bak
    else
      printf '\nARTEX_TAG=%s\n' "$tag" >> .env
    fi
    ok "Set ARTEX_TAG to ${tag}"
  fi

  # Update only artex. PostgreSQL is pinned to 16-alpine; pulling it again wastes bandwidth and
  # changing major versions risks incompatibility. Since artex declares depends_on postgres,
  # targeting artex with up starts PostgreSQL if needed and leaves a running instance unchanged.
  info "Pulling the new image (artex only)..."
  if ! docker compose pull artex; then
    warn "Could not pull ghcr.io/jiwoochris/artex-global (check that the tag is a published release tag and the network is available)"
    die "Image update failed. You can also use \"2) Local update (rebuild with Go)\""
  fi
  info "Recreating and starting (artex migrates the schema automatically on restart)..."
  docker compose up -d artex
  ok "Update complete -> http://localhost:8787"
  info "View logs: docker compose logs -f artex"
  info "Remove old images (optional): docker image prune -f"
}

# -- 2) Local build update --
update_local(){
  command -v go >/dev/null 2>&1 || die "Go was not found (>=1.26 required): https://go.dev/dl/"
  [ -f config.json ] || warn "config.json was not found. Use ./install.sh for an initial deployment"
  ok "Go: $(go version)"

  if command -v npm >/dev/null 2>&1; then
    info "Rebuilding static frontend assets..."
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "Recompiling a single binary with the embedded frontend..."
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm was not found. Building only the backend without an embedded frontend (run the frontend separately with npm run dev)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "Build complete -> ./artex"
  warn "Restart the running artex process to apply changes (schema migrations run automatically on restart)"
}

echo "=============================="
echo "  ARTEX update"
echo "  1) Docker update (pull and recreate)"
echo "  2) Local update (rebuild with Go)"
echo "=============================="
case "$(ask 'Select' 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "Invalid selection" ;;
esac
