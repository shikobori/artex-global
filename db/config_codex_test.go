package db

import "testing"

func TestCodexProfileClearsCredentialsAndEntersPool(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	p := &LLMProfile{Name: "test-codex-transition", Format: "openai", Model: "test-model", APIKey: "old-secret", Streaming: true}
	id, err := d.SaveProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM llm_profiles WHERE id=$1`, id)
	p.ID = id
	p.Format = "codex"
	p.APIKey = "must-not-be-stored"
	p.Proxy = "http://untrusted"
	p.BaseURL = "https://untrusted"
	p.Streaming = false
	p.MaxTokens = 123
	if _, err = d.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	stored, err := d.ProfileByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "" || stored.Proxy != "" || stored.BaseURL != "" || !stored.Streaming || stored.MaxTokens != 0 {
		t.Fatal("Codex profile retained unsafe or unsupported settings")
	}
	profiles, err := d.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range profiles {
		if v.ID == id && v.APIKeyHint != "" {
			t.Fatal("old key hint persisted")
		}
	}
	pool, err := d.PoolProfiles()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range pool {
		if v.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("keyless Codex profile excluded from pool")
	}
	stored.Format = "openai"
	stored.APIKey = "replacement-key"
	if _, err = d.SaveProfile(stored); err != nil {
		t.Fatal(err)
	}
	stored.APIKey = ""
	if _, err = d.SaveProfile(stored); err != nil {
		t.Fatal(err)
	}
	stored, err = d.ProfileByID(id)
	if err != nil || stored.APIKey != "replacement-key" {
		t.Fatal("API-key blank preservation regressed")
	}
}
