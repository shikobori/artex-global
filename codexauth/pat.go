package codexauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// This is the metadata endpoint used by `codex login --with-access-token`.
// PATs are opaque: obtain the workspace from OpenAI, never from caller input.
const patMetadataURL = "https://auth.openai.com/api/accounts/v1/user-auth-credential/whoami"

func personalAccessCredential(token string) (credential, error) {
	if !strings.HasPrefix(token, "at-") || len(token) <= 3 || len(token) > 64*1024 {
		return credential{}, errors.New("CODEX_ACCESS_TOKEN must be a Codex personal access token beginning with at-")
	}
	for _, c := range token {
		if c <= 32 || c >= 127 {
			return credential{}, errors.New("Codex personal access token contains invalid characters")
		}
	}
	return credential{access: token, personalAccessToken: true}, nil
}

func lookupPATAccount(ctx context.Context, token string) (string, error) {
	return fetchPATAccount(ctx, token, patHTTPClient())
}

func patHTTPClient() *http.Client {
	return &http.Client{
		Transport:     directTransport(),
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func fetchPATAccount(ctx context.Context, token string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", patMetadataURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "artex")
	req.Header.Set("User-Agent", "artex-codex-provider")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("could not validate Codex personal access token with OpenAI")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never expose a response body that may echo the token.
		return "", errors.New("OpenAI rejected the Codex personal access token metadata request; check token validity, Codex scope, and workspace permissions")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return "", errors.New("could not read Codex personal access token metadata")
	}
	var metadata struct {
		Account string `json:"chatgpt_account_id"`
		FedRAMP bool   `json:"chatgpt_account_is_fedramp"`
	}
	if json.Unmarshal(raw, &metadata) != nil || strings.TrimSpace(metadata.Account) == "" || strings.ContainsAny(metadata.Account, "\r\n") {
		return "", errors.New("invalid Codex personal access token metadata")
	}
	if metadata.FedRAMP {
		return "", errors.New("FedRAMP workspaces are not supported by this Codex endpoint")
	}
	return metadata.Account, nil
}
