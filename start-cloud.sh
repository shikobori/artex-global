#!/usr/bin/env bash
set -euo pipefail

# Render assigns the PORT environment variable dynamically; pass it to the app.
# Keep the default startup behavior and skill/data paths.
: "${PORT:?The PORT environment variable must be set}"
echo "[*] Starting ARTEX Server on port :$PORT"
exec ./artex -addr ":$PORT"
