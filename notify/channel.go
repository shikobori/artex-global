package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel adapts a notification channel. Implementations must be stateless: the same instance is reused
// concurrently across configurations, and all credentials come from cfg.
type Channel interface {
	// Kind returns the channel type identifier, which must match its registry key.
	Kind() string
	// Validate checks required fields and formats when saving configuration. Its errors are shown directly to
	// users, so identify the missing field rather than saying only that the configuration is invalid.
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the number of items actually delivered and an error. Platforms
	// limit message length, so a digest may omit some items. Mark only the first kept items as delivered and
	// leave the rest for the next batch; otherwise omitted findings disappear while their history incorrectly
	// reports success. An error indicates failure; *PermanentError means do not retry. Ignore kept on failure.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the platform's recommended deliveries per minute as the default rate limit for
	// new channel instances. Zero means no known limit.
	DefaultRatePerMin() int
	// SecretKeys returns credential keys. The API masks their values and preserves stored values when updates
	// contain a mask. Only the channel knows which fields are credentials (the entire WeCom Webhook URL, for
	// example, versus DingTalk's secret); callers must not guess.
	SecretKeys() []string
	// DestinationKeys returns keys that determine where messages are sent. Like SecretKeys, this is security-
	// sensitive: allowing an address change while preserving credentials lets a configuration editor forward
	// stored secrets to their own server, defeating masking. See PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry explicitly lists channels instead of using init-based registration. This keeps the supported set
// visible in one place and exposes omissions at compile time rather than relying on runtime side effects.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the implementation for a channel type.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel type.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported channel types in lexical order for stable UI dropdowns.
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that should not be retried, such as invalid credentials,
// destination rejection, or an invalid body. Retries help transient network failures, throttling, and
// remote 5xx responses; retrying permanent failures only buries the real error in retry logs.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. It returns nil for nil, allowing return
// Permanent(someCheck()).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether the error chain contains a permanent failure marker.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// Configuration helpers. Database JSONB becomes map[string]any through encoding/json, with float64 numbers
// and []any arrays. These helpers normalize conversions and tolerate UI type variations, such as a string
// port.

// cfgString reads a string value and trims whitespace commonly introduced by copying form values.
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt reads an integer from either float64 (the JSON default) or a string.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool reads a boolean, also accepting the strings "true" and "1".
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings reads a string array, trims whitespace, and removes empty entries.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Also accept a single string for forms with just one value.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a string map, such as custom HTTP headers, trimming keys and values and dropping empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}
