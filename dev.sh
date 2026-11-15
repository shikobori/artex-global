#!/usr/bin/env bash
# Development mode: run the backend (127.0.0.1:8787) and traffic proxy (:8788) with next dev (:5173).
# Frontend /api requests are reverse-proxied to the backend. Ctrl-C stops both processes.
#
# For a single binary with an embedded frontend, see the single-binary section in README instead of using this script.
set -euo pipefail
cd "$(dirname "$0")"

# Stop all child processes in this process group (backend and frontend) on exit.
cleanup() { kill 0 2>/dev/null || true; }
trap cleanup EXIT INT TERM

# Backend (plain go run, without an embedded frontend). Configure concurrent work agents in System Settings.
go run ./cmd/artex -addr 127.0.0.1:8787 -proxy 127.0.0.1:8788 &

# Frontend hot reload (Vite/Next dev server, with /api reverse-proxied to :8787).
( cd web && npm run dev ) &

echo "[dev] Backend :8787 / Proxy :8788 / Frontend http://localhost:5173  (Ctrl-C to stop)"
wait
