// Package codexauth reuses the local Codex CLI's ChatGPT login. It never copies
// credentials into ARTEX profiles or returns them to the browser.
package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Format = "codex"
const BaseURL = "https://chatgpt.com/backend-api/codex"

// Source deliberately does not expose the refresh token. Codex owns rotation
// and persistence, including compatibility with newer auth.json formats.
type Source struct {
	path          string
	refresh       func(context.Context) error
	mu            sync.Mutex
	useEnv        bool
	lookupPAT     func(context.Context, string) (string, error)
	patCredential credential
}
type credential struct {
	access, account     string
	personalAccessToken bool
}

var defaultMu sync.Mutex
var defaultSources = map[string]*Source{}

func DefaultSource() (*Source, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(h, ".codex")
	}
	path, err := filepath.Abs(filepath.Join(home, "auth.json"))
	if err != nil {
		return nil, err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if s := defaultSources[path]; s != nil {
		return s, nil
	}
	s := &Source{path: path, refresh: refreshCLI, useEnv: true, lookupPAT: lookupPATAccount}
	defaultSources[path] = s
	return s, nil
}
func (s *Source) read() (credential, error) {
	var c credential
	if token := os.Getenv("CODEX_ACCESS_TOKEN"); s.useEnv && token != "" {
		return personalAccessCredential(token)
	}
	f, err := os.Open(s.path)
	if err != nil {
		return c, errors.New("Codex auth.json is unavailable; sign in with codex login or configure CODEX_ACCESS_TOKEN")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return c, errors.New("could not read Codex credentials")
	}
	var auth struct {
		Mode   string  `json:"auth_mode"`
		PAT    *string `json:"personal_access_token"`
		APIKey string  `json:"OPENAI_API_KEY"`
		Tokens struct {
			Access  string `json:"access_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return c, errors.New("invalid Codex auth.json; sign in again with codex login")
	}
	if auth.PAT != nil {
		if (auth.Mode != "" && auth.Mode != "personal_access_token") || auth.APIKey != "" || auth.Tokens.Access != "" || auth.Tokens.Account != "" {
			return c, errors.New("Codex auth.json contains conflicting authentication methods")
		}
		return personalAccessCredential(*auth.PAT)
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" {
		return c, errors.New("Codex must be signed in with ChatGPT; use an OpenAI profile for API-key authentication")
	}
	if auth.Tokens.Access == "" || auth.Tokens.Account == "" || strings.ContainsAny(auth.Tokens.Access+auth.Tokens.Account, "\r\n") {
		return c, errors.New("no usable ChatGPT login in Codex auth.json; run codex login")
	}
	return credential{access: auth.Tokens.Access, account: auth.Tokens.Account}, nil
}
func expiresSoon(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(data, &claims) != nil || claims.Exp == 0 {
		return false
	}
	// This unverified claim is only a refresh hint, never an identity decision.
	return time.Until(time.Unix(claims.Exp, 0)) < time.Minute
}
func (s *Source) get(ctx context.Context, rejected string) (credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.read()
	if err != nil {
		return c, err
	}
	if c.personalAccessToken {
		return s.resolvePAT(ctx, c)
	}
	s.patCredential = credential{}
	if (rejected != "" && c.access == rejected) || expiresSoon(c.access) {
		if err = s.refresh(ctx); err != nil {
			return credential{}, err
		}
		c, err = s.read()
		if err == nil && c.personalAccessToken {
			return s.resolvePAT(ctx, c)
		}
	}
	return c, err
}

func (s *Source) resolvePAT(ctx context.Context, c credential) (credential, error) {
	if s.patCredential.access == c.access {
		return s.patCredential, nil
	}
	account, err := s.lookupPAT(ctx, c.access)
	if err != nil {
		return credential{}, err
	}
	c.account = account
	s.patCredential = c
	return c, nil
}

// Available only checks the presence and shape of cached credentials. It does
// not expose a token, refresh the login, or make a billable inference request.
func Available() error {
	s, err := DefaultSource()
	if err != nil {
		return err
	}
	_, err = s.read()
	return err
}
