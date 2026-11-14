package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, token string) *Source {
	t.Helper()
	s := &Source{path: filepath.Join(t.TempDir(), "auth.json")}
	writeAuth(t, s.path, token)
	return s
}
func writeAuth(t *testing.T, path, token string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"access_token": token, "account_id": "account-fixture", "refresh_token": "never-export-me"}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestReadCodexCache(t *testing.T) {
	s := fixture(t, "fixture-token")
	c, err := s.read()
	if err != nil || c.access != "fixture-token" || c.account != "account-fixture" {
		t.Fatalf("read failed: %v", err)
	}
	for _, raw := range []string{`{`, `{"auth_mode":"apikey","OPENAI_API_KEY":"secret"}`, `{"auth_mode":"chatgpt","tokens":{"access_token":"secret"}}`, `{"tokens":{"access_token":"x\r\ny","account_id":"a"}}`} {
		os.WriteFile(s.path, []byte(raw), 0600)
		_, err := s.read()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe read result: %v", err)
		}
	}
}
func TestCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeAuth(t, filepath.Join(home, "auth.json"), "fixture")
	s, err := DefaultSource()
	if err != nil || s.path != filepath.Join(home, "auth.json") {
		t.Fatalf("wrong source: %v", err)
	}
	if err = Available(); err != nil {
		t.Fatal(err)
	}
}
func TestRefreshSerializedAndRereadsCache(t *testing.T) {
	s := fixture(t, "old")
	var calls atomic.Int32
	s.refresh = func(context.Context) error { calls.Add(1); writeAuth(t, s.path, "new"); return nil }
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.get(context.Background(), "old")
			if err != nil || c.access != "new" {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refreshed %d times", calls.Load())
	}
	writeAuth(t, s.path, "changed-by-cli")
	c, err := s.get(context.Background(), "")
	if err != nil || c.access != "changed-by-cli" {
		t.Fatalf("stale cached token: %v", err)
	}
}
func TestExpiryRefreshAndFailure(t *testing.T) {
	exp := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(-time.Minute).Unix())))
	s := fixture(t, "header."+exp+".sig")
	s.refresh = func(context.Context) error { return fmt.Errorf("please sign in") }
	if _, err := s.get(context.Background(), ""); err == nil {
		t.Fatal("used expired token after refresh failed")
	}
}
