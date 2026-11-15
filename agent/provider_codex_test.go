package agent

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/Autumn-27/artex/codexauth"
	"github.com/Autumn-27/norma/llm"
)

func TestCodexConfigDoesNotRetainProfileSecretsOrDestinations(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-token","account_id":"test-account"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := ConfigFrom("codex", "test-model", "https://attacker.invalid/responses", "profile-secret", "http://attacker.invalid:8080")
	if c.APIKey != "" || c.Proxy != "" || c.BaseURL != codexauth.BaseURL || c.Provider() != "codex" || !c.HasCredentials() {
		t.Fatal("Codex profile retained an unsafe setting")
	}
	if _, err := c.NewProvider(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARTEX_LLM_PROVIDER", "codex")
	t.Setenv("ARTEX_LLM_MODEL", "test-model")
	t.Setenv("OPENAI_API_KEY", "unrelated-secret")
	t.Setenv("ARTEX_LLM_BASE_URL", "https://attacker.invalid")
	t.Setenv("ARTEX_LLM_PROXY", "http://attacker.invalid")
	env, ok := FromEnv()
	if !ok || env.APIKey != "" || env.Proxy != "" || env.BaseURL != codexauth.BaseURL {
		t.Fatal("unsafe environment configuration")
	}
	os.Remove(filepath.Join(dir, "auth.json"))
	if _, err := c.NewProvider(); err == nil {
		t.Fatal("missing login accepted")
	}
}

type codexStreamFixture struct{}

func (codexStreamFixture) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "OK"}, nil) {
			return
		}
		yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "end_turn", Usage: llm.Usage{OutputTokens: 1}}, nil)
	}
}
func (codexStreamFixture) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	panic("must not use non-streaming Codex API")
}
func TestCodexCompleteAccumulatesStream(t *testing.T) {
	p := codexProvider{codexStreamFixture{}}
	m, stop, usage, err := p.Complete(context.Background(), llm.CompletionRequest{})
	if err != nil || stop != "end_turn" || usage.OutputTokens != 1 || len(m.Content) != 1 || m.Content[0].Text != "OK" {
		t.Fatalf("bad completion: %v %v", m, err)
	}
}

func TestCodexPersonalAccessTokenFromEnv(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("CODEX_ACCESS_TOKEN", "at-env-fixture")
	t.Setenv("ARTEX_LLM_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ARTEX_LLM_MODEL", "fixture-model")
	c, ok := FromEnv()
	if !ok || c.Provider() != "codex" || c.APIKey != "" {
		t.Fatal("PAT was not selected or leaked into config")
	}
	if _, err := c.NewProvider(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARTEX_LLM_PROVIDER", "openai")
	if _, ok := FromEnv(); ok {
		t.Fatal("PAT must not authenticate an OpenAI Platform profile")
	}
}
