"""Import a Compose-provided Codex login, then exec the normal ARTEX supervisor."""

import fcntl
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile


class CredentialError(ValueError):
    """Only fixed, credential-free messages may be surfaced at startup."""


def atomic_write(path: Path, data: bytes) -> None:
    fd, temporary = tempfile.mkstemp(prefix=".auth-import-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            os.fchmod(output.fileno(), 0o600)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def import_auth(raw: str, home: Path) -> None:
    if len(raw.encode("utf-8")) > 1024 * 1024:
        raise CredentialError("ARTEX_CODEX_AUTH_JSON is too large")
    try:
        auth = json.loads(raw)
    except (ValueError, RecursionError):
        raise CredentialError("ARTEX_CODEX_AUTH_JSON must contain the complete, valid auth.json JSON") from None
    if not isinstance(auth, dict) or auth.get("auth_mode") not in (None, "chatgpt"):
        raise CredentialError("ARTEX_CODEX_AUTH_JSON must contain a Codex ChatGPT login")
    tokens = auth.get("tokens")
    if not isinstance(tokens, dict) or any(
        not isinstance(tokens.get(key), str)
        or not tokens[key].strip()
        or "\r" in tokens[key]
        or "\n" in tokens[key]
        for key in ("access_token", "refresh_token", "account_id")
    ):
        raise CredentialError("Codex auth.json is missing valid access_token, refresh_token, or account_id fields")
    # Canonicalize so changing whitespace/key order in .env does not roll back
    # a session that Codex has already refreshed.
    data = json.dumps(auth, sort_keys=True, separators=(",", ":")).encode("utf-8")
    fingerprint = hashlib.sha256(data).hexdigest().encode("ascii")
    if home.is_symlink():
        raise CredentialError("CODEX_HOME must not be a symbolic link")
    home.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(home, 0o700)
    lock_fd = os.open(home / ".artex-auth-import.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, "wb") as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        marker = home / ".artex-auth-import.sha256"
        try:
            previous = marker.read_bytes()
        except FileNotFoundError:
            previous = None
        if previous == fingerprint:
            # Also preserve an intentional `codex logout` (missing auth.json).
            return
        atomic_write(home / "auth.json", data)
        atomic_write(marker, fingerprint)


def prepare_environment(environ: dict) -> None:
    # Remove the entire seed from the supervisor/ARTEX/tool environment. Docker
    # still retains its configured environment; Docker-admin access is trusted.
    raw = environ.pop("ARTEX_CODEX_AUTH_JSON", "")
    if not raw.strip():
        return
    home = Path(environ.get("CODEX_HOME") or "/app/data/codex")
    environ["CODEX_HOME"] = str(home)
    import_auth(raw, home)
    if not environ.get("ARTEX_LLM_PROVIDER"):
        environ["ARTEX_LLM_PROVIDER"] = "codex"


def main() -> int:
    try:
        prepare_environment(os.environ)
    except CredentialError as error:
        print(f"[artex] {error}", file=sys.stderr)
        return 1
    except (OSError, ValueError):
        print("[artex] Could not persist Codex credentials; check CODEX_HOME permissions and disk space", file=sys.stderr)
        return 1
    os.execvpe("/app/start.sh", ["/app/start.sh", *sys.argv[1:]], dict(os.environ))
    return 0


if __name__ == "__main__":
    sys.exit(main())
