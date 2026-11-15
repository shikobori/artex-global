package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// These tests define Webhook template capability boundaries. This is the only place in the package where
// user strings are evaluated as code; adding context methods or FuncMap functions such as readFile must not
// silently expand capabilities behind an apparently harmless change.

// TestTemplateContextHasNoMethods enforces pure-data contexts. text/template can call exported methods
// through expressions such as {{.Foo}}, so any reachable type with exported methods exposes them to
// template authors. A failure means webhookTemplateData or webhookItem gained a method; review whether it
// exposes unintended data before allowing it.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s): text/template can call them,"+
				" exposing their capabilities to template authors", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// TestTemplateFuncsAreMinimal fixes the exposed function set. Each FuncMap entry adds a capability;
// json/jsons only serialize JSON fragments and cannot read files, send requests, or execute commands.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Template function set changed: got %v, want %v. Ensure new functions do not expand capabilities"+
			" (no filesystem access, network requests, or command execution)", got, want)
	}
}

// TestTemplateCannotReachUnknownData requires unknown field access to fail without exposing internal data
// in its error.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, singleMsg())
	if err == nil {
		t.Fatal("Unknown field access should fail")
	}
	// Errors must not contain actual finding titles or summaries from the template context.
	for _, leak := range []string{"SQL注入", "参数 id"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("Template error leaked message content %q: %v", leak, err)
		}
	}
}

// TestTemplateRenderFailsPermanently treats bad templates as configuration failures. Retrying would waste
// three backoff rounds on every delivery without fixing them.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("Template syntax errors should be rejected on save")
	}
	// Even when validation is bypassed, direct delivery must fail permanently rather than retry.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("Invalid template should fail permanently, got %v", err)
	}
}

// TestTemplateCanOnlyProduceJSON requires valid JSON output, also preventing templates from generating
// plaintext to trigger other protocols.
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// A valid template passes.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("Valid template should pass validation: %v", err)
	}
	// Reject non-JSON output instead of sending it unchanged.
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("Non-JSON output should fail permanently, got %v", err)
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("Error should explain the JSON problem, got %v", err)
	}
}
