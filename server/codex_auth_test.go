package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexStatusNeverReturnsCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("CODEX_ACCESS_TOKEN", "")
	t.Setenv("PATH", dir)
	// Status only locates the CLI; it must not execute it or refresh credentials.
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `{"auth_mode":"chatgpt","tokens":{"access_token":"access-fixture-secret","account_id":"account-fixture","refresh_token":"refresh-fixture-secret"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	w := httptest.NewRecorder()
	s.codexStatus(w, httptest.NewRequest("GET", "/api/llm/codex/status", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"connected":true`) {
		t.Fatalf("unexpected status: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fixture") {
		t.Fatal("credential details leaked")
	}
	os.Remove(filepath.Join(dir, "auth.json"))
	w = httptest.NewRecorder()
	s.codexStatus(w, httptest.NewRequest("GET", "/api/llm/codex/status", nil))
	if !strings.Contains(w.Body.String(), `"connected":false`) {
		t.Fatal("missing login reported as connected")
	}
	t.Setenv("CODEX_ACCESS_TOKEN", "at-env-fixture-secret")
	w = httptest.NewRecorder()
	s.codexStatus(w, httptest.NewRequest("GET", "/api/llm/codex/status", nil))
	if !strings.Contains(w.Body.String(), `"connected":true`) || strings.Contains(w.Body.String(), "fixture") {
		t.Fatal("PAT status missing or credential details leaked")
	}
}
