package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const complete = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"

func TestTransportPinsDestinationAndRefreshes401(t *testing.T) {
	s := fixture(t, "old")
	s.refresh = func(context.Context) error { writeAuth(t, s.path, "new"); return nil }
	calls := 0
	tr := &Transport{Source: s, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		want := "Bearer old"
		if calls > 1 {
			want = "Bearer new"
		}
		if r.Header.Get("Authorization") != want || r.Header.Get("ChatGPT-Account-Id") != "account-fixture" {
			t.Fatal("missing auth")
		}
		if r.Header.Get("X-Untrusted") != "" {
			t.Fatal("forwarded caller headers")
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		if body["store"] != false || body["stream"] != true || body["max_output_tokens"] != nil || body["temperature"] != nil {
			t.Fatalf("invalid request: %s", raw)
		}
		if calls == 1 {
			return response(401, "unauthorized"), nil
		}
		return response(200, complete), nil
	})}
	req, _ := http.NewRequest("POST", BaseURL+"/responses", strings.NewReader(`{"model":"test","store":true,"stream":false,"input":[],"max_output_tokens":20,"temperature":1}`))
	req.Header.Set("X-Untrusted", "evil")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err = io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || resp.Request.Header.Get("Authorization") != "" {
		t.Fatal("incorrect retry or token leaked via response.Request")
	}
	for _, u := range []string{"https://evil.example/responses", BaseURL + "/responses?redirect=1", "http://chatgpt.com/backend-api/codex/responses", BaseURL + "/models"} {
		req, _ = http.NewRequest("POST", u, strings.NewReader(`{}`))
		if _, err = tr.RoundTrip(req); err == nil {
			t.Errorf("allowed %s", u)
		}
	}
	if calls != 2 {
		t.Fatal("sent an untrusted request")
	}
}
func TestRedirectRejectedAndErrorsRedacted(t *testing.T) {
	for _, status := range []int{302, 403} {
		tr := &Transport{Source: fixture(t, "fixture-secret"), Base: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(status, "echo fixture-secret"), nil })}
		req, _ := http.NewRequest("POST", BaseURL+"/responses", strings.NewReader(`{"input":[]}`))
		resp, err := tr.RoundTrip(req)
		if status == 302 {
			if err == nil {
				t.Fatal("redirect accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if bytes.Contains(raw, []byte("fixture-secret")) {
			t.Fatal("error leaked token")
		}
	}
}
func TestStreamRequiresSuccessfulTerminalEvent(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"complete", complete, true},
		{"data only", `data: {"type":"response.completed"}` + "\n\n", true},
		{"truncated", `event: response.output_text.delta` + "\n" + `data: {"delta":"partial"}` + "\n\n", false},
		{"failed", `event: response.failed` + "\n" + `data: {"response":{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}}` + "\n\n", false},
		{"incomplete", `event: response.incomplete` + "\n" + `data: {"response":{"status":"incomplete"}}` + "\n\n", false},
		{"invalid", "data: not-json\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := checkedStream(io.NopCloser(strings.NewReader(tc.body)))
			defer b.Close()
			_, err := io.ReadAll(b)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestAdaptPreservesARTEXToolCalls(t *testing.T) {
	raw := []byte(`{"model":"test","tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],"input":[{"type":"function_call","call_id":"call1","name":"shell","arguments":"{}"},{"type":"function_call_output","call_id":"call1","output":"ok"}]}`)
	body, err := adaptRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(body, &got)
	if len(got["tools"].([]any)) != 1 || len(got["input"].([]any)) != 2 {
		t.Fatal("lost tools or history")
	}
}
