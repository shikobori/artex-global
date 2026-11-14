package codexauth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}
type rpcClient struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	scan *bufio.Scanner
	next int
}

func startCLI(ctx context.Context) (*rpcClient, error) {
	// No shell, no model turn, and no secrets in argv or environment additions.
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://", "-c", `model_provider="openai"`, "-c", `cli_auth_credentials_store="file"`)
	cmd.Stderr = io.Discard
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "OPENAI_API_KEY=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		return nil, errors.New("codex CLI is unavailable on PATH")
	}
	c := &rpcClient{cmd: cmd, in: in, scan: bufio.NewScanner(out)}
	c.scan.Buffer(make([]byte, 4096), 4<<20)
	if _, err = c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "artex", "version": "1.0"}, "capabilities": map[string]bool{"experimentalApi": false}}); err != nil {
		c.close()
		return nil, err
	}
	if err = json.NewEncoder(in).Encode(map[string]any{"method": "initialized"}); err != nil {
		c.close()
		return nil, errors.New("Codex initialization failed")
	}
	return c, nil
}
func (c *rpcClient) close() {
	c.in.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
}
func (c *rpcClient) call(method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := json.NewEncoder(c.in).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, errors.New("could not communicate with Codex")
	}
	for c.scan.Scan() {
		var msg rpcMessage
		if json.Unmarshal(c.scan.Bytes(), &msg) != nil {
			continue
		}
		if msg.Method != "" {
			// This integration never approves actions or supplies external credentials.
			if len(msg.ID) > 0 {
				json.NewEncoder(c.in).Encode(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Unsupported by ARTEX authentication client"}})
			}
			continue
		}
		var got int
		if json.Unmarshal(msg.ID, &got) != nil || got != id {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return nil, fmt.Errorf("Codex %s failed; check codex login status", method)
		}
		return msg.Result, nil
	}
	return nil, errors.New("Codex authentication connection closed or timed out")
}
func refreshCLI(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := startCLI(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	raw, err := c.call("account/read", map[string]bool{"refreshToken": true})
	if err != nil {
		return err
	}
	var v struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Account == nil || v.Account.Type != "chatgpt" {
		return errors.New("Codex is not signed in with ChatGPT; run codex login")
	}
	return nil
}

// Models uses Codex's account-aware catalog, not a user-controlled HTTP URL.
func Models(ctx context.Context) ([]string, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := startCLI(ctx)
	if err != nil {
		return nil, err
	}
	defer c.close()
	var models []string
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 20; page++ {
		raw, err := c.call("model/list", map[string]any{"cursor": cursorOrNil(cursor), "limit": 100, "includeHidden": false})
		if err != nil {
			return nil, err
		}
		var v struct {
			Data []struct {
				Model  string `json:"model"`
				Hidden bool   `json:"hidden"`
			} `json:"data"`
			Next *string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return nil, errors.New("invalid Codex model catalog")
		}
		for _, m := range v.Data {
			if m.Model != "" && !m.Hidden && !seen[m.Model] {
				models = append(models, m.Model)
				seen[m.Model] = true
			}
		}
		if v.Next == nil || *v.Next == "" {
			return models, nil
		}
		if *v.Next == cursor {
			return nil, errors.New("invalid Codex model cursor")
		}
		cursor = *v.Next
	}
	return nil, errors.New("Codex model catalog exceeded pagination limit")
}
func cursorOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
