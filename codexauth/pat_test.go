package codexauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func writePAT(t *testing.T, path, token string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"OPENAI_API_KEY": nil, "personal_access_token": token})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPATCredentialSourcesAndValidation(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CODEX_ACCESS_TOKEN", "")
	s, err := DefaultSource()
	if err != nil {
		t.Fatal(err)
	}
	writePAT(t, s.path, "at-cache-fixture")
	c, err := s.read()
	if err != nil || !c.personalAccessToken || c.access != "at-cache-fixture" || c.account != "" {
		t.Fatalf("PAT cache not recognized: %v", err)
	}
	// Availability does not make a network request for metadata.
	if err := Available(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_ACCESS_TOKEN", "at-env-fixture")
	c, err = s.read()
	if err != nil || c.access != "at-env-fixture" {
		t.Fatal("environment token must take precedence")
	}
	for _, token := range []string{"sk-secret", "at-", "at-secret\r\n", "at-secret value", "at-secret\t", "at-secret\x7f", "at-secret日本", "at-" + strings.Repeat("s", 64*1024)} {
		t.Setenv("CODEX_ACCESS_TOKEN", token)
		if _, err := s.read(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid PAT accepted or exposed: %v", err)
		}
	}
	t.Setenv("CODEX_ACCESS_TOKEN", "")
	for _, raw := range []string{
		`{"personal_access_token":42}`,
		`{"personal_access_token":"at-secret","auth_mode":"chatgpt"}`,
		`{"personal_access_token":"at-secret","OPENAI_API_KEY":"sk-secret"}`,
		`{"personal_access_token":"at-secret","tokens":{"access_token":"oauth-secret"}}`,
	} {
		os.WriteFile(s.path, []byte(raw), 0600)
		if _, err := s.read(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("conflicting credentials accepted or exposed: %v", err)
		}
	}
}

func TestPATMetadataSerializedAndRotation(t *testing.T) {
	s := &Source{path: filepath.Join(t.TempDir(), "auth.json")}
	writePAT(t, s.path, "at-first")
	var calls atomic.Int32
	s.lookupPAT = func(_ context.Context, token string) (string, error) {
		calls.Add(1)
		return "account-" + strings.TrimPrefix(token, "at-"), nil
	}
	s.refresh = func(context.Context) error { t.Error("PAT must not refresh OAuth"); return nil }
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.get(context.Background(), "")
			if err != nil || c.account != "account-first" || !c.personalAccessToken {
				t.Errorf("metadata resolution failed: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("metadata lookup was not shared")
	}
	writePAT(t, s.path, "at-second")
	c, err := s.get(context.Background(), "")
	if err != nil || c.account != "account-second" || calls.Load() != 2 {
		t.Fatal("token rotation reused old metadata")
	}
	writeAuth(t, s.path, "oauth-fixture")
	c, err = s.get(context.Background(), "")
	if err != nil || c.personalAccessToken || c.account != "account-fixture" {
		t.Fatal("switching back to OAuth failed")
	}
}

func TestPATMetadataEndpointAndSafeFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{"valid", 200, `{"chatgpt_account_id":"workspace","email":"private@example.invalid"}`, true},
		{"expired", 401, `at-secret`, false},
		{"forbidden", 403, `at-secret`, false},
		{"redirect", 302, `at-secret`, false},
		{"invalid", 200, `at-secret`, false},
		{"missing account", 200, `{}`, false},
		{"header injection", 200, `{"chatgpt_account_id":"x\r\nInjected: yes"}`, false},
		{"fedramp", 200, `{"chatgpt_account_id":"workspace","chatgpt_account_is_fedramp":true}`, false},
		{"oversized", 200, strings.Repeat("x", 1<<20+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := patHTTPClient()
			if client.Transport.(*http.Transport).Proxy != nil {
				t.Fatal("metadata must not use an environment proxy")
			}
			calls := 0
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != patMetadataURL || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer at-secret" {
					t.Fatal("incorrect credential destination")
				}
				resp := response(tc.status, tc.body)
				if tc.status == 302 {
					resp.Header.Set("Location", "https://attacker.invalid/")
				}
				return resp, nil
			})
			account, err := fetchPATAccount(context.Background(), "at-secret", client)
			if (err == nil) != tc.ok || calls != 1 || (tc.ok && account != "workspace") {
				t.Fatalf("unexpected metadata result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "at-secret") {
				t.Fatal("metadata error exposed token")
			}
		})
	}
}

func TestPATTransportDoesNotRefreshOrFallback(t *testing.T) {
	s := &Source{path: filepath.Join(t.TempDir(), "auth.json")}
	writePAT(t, s.path, "at-secret")
	s.lookupPAT = func(context.Context, string) (string, error) { return "workspace", nil }
	s.refresh = func(context.Context) error { t.Fatal("PAT rejection attempted OAuth refresh"); return nil }
	calls := 0
	tr := &Transport{Source: s, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer at-secret" || r.Header.Get("ChatGPT-Account-Id") != "workspace" {
			t.Fatal("incorrect inference credentials")
		}
		return response(401, "rejected at-secret"), nil
	})}
	req, _ := http.NewRequest("POST", BaseURL+"/responses", strings.NewReader(`{"input":[]}`))
	resp, err := tr.RoundTrip(req)
	if err != nil || resp.StatusCode != 401 || calls != 1 {
		t.Fatalf("unexpected retry: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "at-secret") {
		t.Fatal("error exposed PAT")
	}
	// A failed metadata lookup must prevent any inference request.
	s.patCredential = credential{}
	s.lookupPAT = func(context.Context, string) (string, error) { return "", errors.New("invalid PAT") }
	req, _ = http.NewRequest("POST", BaseURL+"/responses", strings.NewReader(`{"input":[]}`))
	if _, err := tr.RoundTrip(req); err == nil || calls != 1 {
		t.Fatal("sent inference without valid metadata")
	}
}
