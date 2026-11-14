"""Tests use synthetic credentials only; no Docker daemon or Codex login needed."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("entrypoint", Path(__file__).with_name("docker-entrypoint.py"))
entrypoint = importlib.util.module_from_spec(spec)
spec.loader.exec_module(entrypoint)


def fixture(access="synthetic-access"):
    return {"auth_mode": "chatgpt", "tokens": {
        "access_token": access, "refresh_token": "synthetic-refresh", "account_id": "synthetic-account"
    }, "additional_field": "preserve-me"}


class CredentialImportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name) / "codex"

    def prepare(self, data, **env):
        result = {"CODEX_HOME": str(self.home), "ARTEX_CODEX_AUTH_JSON": data, **env}
        entrypoint.prepare_environment(result)
        self.assertNotIn("ARTEX_CODEX_AUTH_JSON", result)
        return result

    def test_import_permissions_unknown_fields_and_provider(self):
        data = fixture()
        env = self.prepare(json.dumps(data, indent=2))
        self.assertEqual(env["ARTEX_LLM_PROVIDER"], "codex")
        self.assertEqual(json.loads((self.home / "auth.json").read_text()), data)
        self.assertEqual(stat.S_IMODE(self.home.stat().st_mode), 0o700)
        for name in ("auth.json", ".artex-auth-import.sha256"):
            self.assertEqual(stat.S_IMODE((self.home / name).stat().st_mode), 0o600)

    def test_restart_preserves_refresh_and_explicit_provider(self):
        self.prepare(json.dumps(fixture()))
        refreshed = fixture("rotated-token")
        (self.home / "auth.json").write_text(json.dumps(refreshed))
        env = self.prepare(json.dumps(fixture(), indent=4, sort_keys=True), ARTEX_LLM_PROVIDER="anthropic")
        self.assertEqual(json.loads((self.home / "auth.json").read_text()), refreshed)
        self.assertEqual(env["ARTEX_LLM_PROVIDER"], "anthropic")

    def test_changed_input_replaces_login(self):
        self.prepare(json.dumps(fixture()))
        fresh = fixture("new-login")
        self.prepare(json.dumps(fresh))
        self.assertEqual(json.loads((self.home / "auth.json").read_text()), fresh)

    def test_logout_is_not_undone_by_restart(self):
        self.prepare(json.dumps(fixture()))
        (self.home / "auth.json").unlink()
        self.prepare(json.dumps(fixture()))
        self.assertFalse((self.home / "auth.json").exists())

    def test_blank_input_does_not_create_directory(self):
        env = self.prepare("  ")
        self.assertFalse(self.home.exists())
        self.assertNotIn("ARTEX_LLM_PROVIDER", env)

    def test_invalid_input_never_clobbers_credentials_or_echoes_secrets(self):
        self.prepare(json.dumps(fixture()))
        before = (self.home / "auth.json").read_bytes()
        for raw in ('{secret', 'null', '[]', '{"tokens": {}}', '{"auth_mode":"apikey"}',
                    '{"tokens":{"access_token":"secret","refresh_token":4,"account_id":"x"}}'):
            with self.subTest(raw=raw):
                with self.assertRaises(entrypoint.CredentialError) as error:
                    self.prepare(raw)
                self.assertNotIn("secret", str(error.exception))
                self.assertEqual((self.home / "auth.json").read_bytes(), before)

    def test_entrypoint_exec_strips_secret_and_preserves_arguments(self):
        env = {"CODEX_HOME": str(self.home), "ARTEX_CODEX_AUTH_JSON": json.dumps(fixture())}
        with patch.dict(os.environ, env, clear=True), patch.object(entrypoint.sys, "argv", ["entrypoint", "-addr", ":8787"]), patch.object(entrypoint.os, "execvpe") as execute:
            self.assertEqual(entrypoint.main(), 0)
            command, arguments, child_env = execute.call_args.args
            self.assertEqual(command, "/app/start.sh")
            self.assertEqual(arguments, ["/app/start.sh", "-addr", ":8787"])
            self.assertNotIn("ARTEX_CODEX_AUTH_JSON", child_env)

    def test_startup_rejects_invalid_input_without_traceback_or_value(self):
        stderr = io.StringIO()
        with patch.dict(os.environ, {"ARTEX_CODEX_AUTH_JSON": "secret-not-json"}, clear=True), patch.object(entrypoint.os, "execvpe") as execute, contextlib.redirect_stderr(stderr):
            self.assertEqual(entrypoint.main(), 1)
            execute.assert_not_called()
        self.assertNotIn("secret-not-json", stderr.getvalue())
        self.assertNotIn("Traceback", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
