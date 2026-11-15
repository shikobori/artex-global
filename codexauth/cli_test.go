package codexauth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIHelper(t *testing.T) {
	if os.Getenv("ARTEX_TEST_CODEX_HELPER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var r struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(scan.Bytes(), &r)
		var result any
		switch r.Method {
		case "initialize":
			result = map[string]any{}
		case "initialized":
			continue
		case "account/read":
			result = map[string]any{"account": map[string]string{"type": "chatgpt"}}
		case "model/list":
			result = map[string]any{"data": []any{map[string]any{"model": "fixture-model", "hidden": false}}, "nextCursor": nil}
		default:
			os.Exit(2)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"id": r.ID, "result": result})
	}
	os.Exit(0)
}
func TestCLIProtocolWithoutRealCredentials(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexec '%s' -test.run=TestCLIHelper -- \"$@\"\n", strings.ReplaceAll(bin, "'", "'\\''"))
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ARTEX_TEST_CODEX_HELPER", "1")
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("CODEX_ACCESS_TOKEN", "")
	writeAuth(t, filepath.Join(dir, "auth.json"), "fixture")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = refreshCLI(ctx); err != nil {
		t.Fatal(err)
	}
	models, err := Models(ctx)
	if err != nil || len(models) != 1 || models[0] != "fixture-model" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	writePAT(t, filepath.Join(dir, "auth.json"), "at-cache-fixture")
	models, err = Models(ctx)
	if err != nil || len(models) != 1 || models[0] != "fixture-model" {
		t.Fatalf("PAT models=%v err=%v", models, err)
	}
	os.Remove(filepath.Join(dir, "auth.json"))
	t.Setenv("CODEX_ACCESS_TOKEN", "at-env-fixture")
	models, err = Models(ctx)
	if err != nil || len(models) != 1 || models[0] != "fixture-model" {
		t.Fatalf("environment PAT models=%v err=%v", models, err)
	}
}
