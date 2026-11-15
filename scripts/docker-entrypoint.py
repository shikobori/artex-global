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


def validate_personal_access_token(token: str) -> None:
    if not isinstance(token, str) or not token.startswith("at-") or not 3 < len(token) <= 64 * 1024:
        raise CredentialError("CODEX_ACCESS_TOKEN must be a Codex personal access token beginning with at-")
    if any(not 33 <= ord(character) <= 126 for character in token):
        raise CredentialError("Codex personal access token contains invalid characters")


def import_auth(raw: str, home: Path) -> None:
    if len(raw.encode("utf-8")) > 1024 * 1024:
        raise CredentialError("ARTEX_CODEX_AUTH_JSON is too large")
    try:
        auth = json.loads(raw)
    except (ValueError, RecursionError):
        raise CredentialError("ARTEX_CODEX_AUTH_JSON must contain the complete, valid auth.json JSON") from None
    if not isinstance(auth, dict):
        raise CredentialError("ARTEX_CODEX_AUTH_JSON must contain a Codex authentication object")
    if auth.get("personal_access_token") is not None:
        validate_personal_access_token(auth["personal_access_token"])
        if auth.get("auth_mode") not in (None, "personal_access_token") or auth.get("tokens") or auth.get("OPENAI_API_KEY"):
            raise CredentialError("Codex auth.json contains conflicting authentication methods")
    else:
        if auth.get("auth_mode") not in (None, "chatgpt"):
            raise CredentialError("ARTEX_CODEX_AUTH_JSON must contain a Codex ChatGPT login or personal access token")
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
    token = environ.pop("CODEX_ACCESS_TOKEN", "")
    if token:
        if raw.strip():
            raise CredentialError("Set only one of CODEX_ACCESS_TOKEN and ARTEX_CODEX_AUTH_JSON")
        validate_personal_access_token(token)
        # Match the credential cache produced by `codex login --with-access-token`.
        raw = json.dumps({"OPENAI_API_KEY": None, "personal_access_token": token})
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
