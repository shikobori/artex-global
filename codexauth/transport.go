package codexauth

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Transport injects the cached token only after checking the exact destination.
// Configure the surrounding client to reject redirects. Profiles cannot change
// the endpoint, proxy, or authentication header for this provider.
type Transport struct {
	Base   http.RoundTripper
	Source *Source
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "POST" || req.URL.String() != BaseURL+"/responses" {
		return nil, errors.New("Codex credentials may only be sent to the fixed Codex Responses endpoint")
	}
	if req.Body == nil {
		return nil, errors.New("Codex request body is required")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, 32<<20+1))
	req.Body.Close()
	if err != nil || len(raw) > 32<<20 {
		return nil, errors.New("invalid or oversized Codex request")
	}
	body, err := adaptRequest(raw)
	if err != nil {
		return nil, err
	}
	cred, err := t.Source.get(req.Context(), "")
	if err != nil {
		return nil, err
	}
	send := func(c credential) (*http.Response, error) {
		r := req.Clone(req.Context())
		r.Header = make(http.Header)
		r.Host = ""
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "text/event-stream")
		r.Header.Set("Authorization", "Bearer "+c.access)
		r.Header.Set("ChatGPT-Account-Id", c.account)
		r.Header.Set("Originator", "artex")
		r.Header.Set("User-Agent", "artex-codex-provider")
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		return t.Base.RoundTrip(r)
	}
	resp, err := send(cred)
	if err != nil {
		return nil, errors.New("Codex inference connection failed")
	}
	if resp.StatusCode == http.StatusUnauthorized && !cred.personalAccessToken {
		resp.Body.Close()
		cred, err = t.Source.get(req.Context(), cred.access)
		if err != nil {
			return nil, err
		}
		resp, err = send(cred)
		if err != nil {
			return nil, errors.New("Codex inference connection failed")
		}
	}
	// Do not return upstream request headers (which contain the token) or allow
	// redirects to turn this into a token forwarding mechanism.
	resp.Request = req
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, errors.New("Codex endpoint redirect refused")
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		// Strip any accidental credential echo before recording or displaying errors.
		raw = bytes.ReplaceAll(raw, []byte(cred.access), []byte("[redacted]"))
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))
		return resp, nil
	}
	resp.Body = checkedStream(resp.Body)
	resp.ContentLength = -1
	return resp, nil
}
func adaptRequest(raw []byte) ([]byte, error) {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, errors.New("invalid Codex Responses body")
	}
	body["store"] = false
	body["stream"] = true
	body["parallel_tool_calls"] = false
	for _, key := range []string{"max_output_tokens", "temperature", "top_p", "previous_response_id", "background", "conversation", "metadata", "prompt_cache_retention", "safety_identifier", "truncation", "user", "max_tool_calls", "moderation", "multi_agent", "prompt", "top_logprobs"} {
		delete(body, key)
	}
	if _, ok := body["instructions"]; !ok {
		body["instructions"] = "You are a helpful assistant."
	}
	input, ok := body["input"].([]any)
	if !ok {
		input = []any{}
	}
	for _, v := range input {
		if item, ok := v.(map[string]any); ok {
			if item["role"] == "system" {
				item["role"] = "developer"
			}
		}
	}
	body["input"] = input
	// Codex accepts flat function tools; keep the SDK's names and call IDs so
	// ARTEX executes its own tools and retains ownership of the agent loop.
	return json.Marshal(body)
}

type streamBody struct {
	*io.PipeReader
	source io.ReadCloser
}

func (s *streamBody) Close() error { s.source.Close(); return s.PipeReader.Close() }

// The SDK treats EOF as success and misses nested response.error. Normalize
// failures and require response.completed, preserving backpressure/cancellation.
func checkedStream(source io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer source.Close()
		scan := bufio.NewScanner(source)
		scan.Buffer(make([]byte, 4096), 16<<20)
		var event string
		var data []string
		frame := func() (bool, error) {
			if len(data) == 0 {
				return false, nil
			}
			payload := strings.Join(data, "\n")
			var obj map[string]json.RawMessage
			if json.Unmarshal([]byte(payload), &obj) != nil {
				return false, errors.New("invalid Codex stream event")
			}
			kind := event
			if kind == "" {
				json.Unmarshal(obj["type"], &kind)
			}
			if kind == "response.failed" || kind == "response.incomplete" || kind == "error" {
				var response struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				json.Unmarshal(obj["response"], &response)
				code := response.Error.Code
				if code == "" {
					var e struct {
						Code string `json:"code"`
					}
					json.Unmarshal(obj["error"], &e)
					code = e.Code
				}
				// Only machine-readable codes; never arbitrary response bodies in errors.
				if len(code) > 100 || strings.ContainsAny(code, "\r\n \t") {
					code = ""
				}
				return false, fmt.Errorf("Codex %s (%s)", kind, code)
			}
			if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", kind, payload); err != nil {
				return false, err
			}
			return kind == "response.completed", nil
		}
		for scan.Scan() {
			line := strings.TrimSuffix(scan.Text(), "\r")
			if line == "" {
				done, err := frame()
				event = ""
				data = nil
				if err != nil {
					writer.CloseWithError(err)
					return
				}
				if done {
					writer.Close()
					return
				}
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if scan.Err() != nil {
			writer.CloseWithError(errors.New("Codex stream read failed"))
			return
		}
		if len(data) > 0 {
			done, err := frame()
			if err != nil {
				writer.CloseWithError(err)
				return
			}
			if done {
				writer.Close()
				return
			}
		}
		writer.CloseWithError(io.ErrUnexpectedEOF)
	}()
	return &streamBody{reader, source}
}

func HTTPClient(source *Source) *http.Client {
	return &http.Client{Transport: &Transport{Base: directTransport(), Source: source}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func directTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.ResponseHeaderTimeout = 30 * time.Second
	return base
}
