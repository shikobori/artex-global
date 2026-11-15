#!/bin/sh
# ARTEX daemon start script (Linux / macOS / Docker ENTRYPOINT)
#
# Usage:
#   ./start.sh                          Run in the foreground (Ctrl-C to stop)
#   nohup ./start.sh >artex.log 2>&1 &   Run in the background
#   ./start.sh -addr :9000              Pass additional arguments directly to artex
#
# This script starts artex and uses its exit code to decide whether to restart it.
#
#   0      Normal user shutdown -> exit the loop
#   75     Restart requested    -> restart immediately (one-click update or rollback in the UI)
#   Other  Abnormal exit        -> restart with backoff (1, 2, 4, ... up to 60 seconds)
#
# Downloads, SHA256 verification, and version replacement are deliberately handled outside this script.
# Duplicating this critical logic in sh and bat would risk installing an unusable binary, which this script
# would then repeatedly try to start until the user manually repaired the installation. All verification
# and replacement therefore live in Go (the selfupdate package), run at startup, keeping this script simple.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] Executable not found: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward shutdown signals to artex.
#
# This is essential in Docker: docker stop sends SIGTERM only to PID 1 (this script),
# not to child processes. Without forwarding, artex cannot shut down gracefully and is
# killed by SIGKILL after 10 seconds, interrupting active tasks.
forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	# A signal interrupts wait with a status greater than 128 while the child is still shutting down.
	# Wait again to obtain its actual exit status.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] Stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] Exited normally"
			exit 0
			;;
		"$RESTART_CODE")
			# Update/rollback is staged. Restarting completes version replacement at startup (see selfupdate.Bootstrap).
			echo "[artex] Restart requested (applying the new version)..."
			delay=1
			;;
		*)
			echo "[artex] Abnormal exit (code=$code), restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done
