package agent

import (
	"context"
	"fmt"

	"github.com/Autumn-27/artex/codexauth"
	"github.com/Autumn-27/norma/llm"
)

func (c Config) newCodexProvider() (llm.Provider, error) {
	if c.Model == "" {
		return nil, fmt.Errorf("Codex model required; choose one from the Codex model list")
	}
	source, err := codexauth.DefaultSource()
	if err != nil {
		return nil, err
	}
	if err = codexauth.Available(); err != nil {
		return nil, err
	}
	client := codexauth.HTTPClient(source)
	// Capture request/response bodies outside the auth-injecting transport. Tokens
	// never appear in the profile config or the recorder's request body.
	client.Transport = quotaAwareTransport{base: client.Transport}
	cfg := llm.Config{Format: llm.FormatOpenAIResponses, BaseURL: codexauth.BaseURL, APIKey: "codex-managed", Model: c.Model, HTTPClient: client, ReasoningEffort: c.ReasoningEffort, MaxRetries: c.Retry.ConnectAttempts, RetryInterval: c.Retry.ConnectInterval}
	if c.RatePerSecond > 0 || c.RatePerMinute > 0 {
		cfg.RateLimit = &llm.RateLimit{PerSecond: c.RatePerSecond, PerMinute: c.RatePerMinute}
	}
	p, err := llm.NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	return codexProvider{p}, nil
}

// Complete is used by compaction and non-streaming consumers. Codex always
// streams on the wire; accumulate that stream instead of sending stream:false.
type codexProvider struct{ llm.Provider }

func (p codexProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	acc := llm.NewAccumulator()
	for ev, err := range p.Stream(ctx, req) {
		if err != nil {
			return llm.Message{}, "", llm.Usage{}, err
		}
		acc.Add(ev)
	}
	return acc.Message(), acc.StopReason, acc.Usage, nil
}
