package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix identifies masked credential values. API responses replace credentials with this
// prefix; updates interpret it as retaining the stored value. A prefix allows an identifying suffix
// (MaskedValue) so users can recognize the bot without repasting secrets.
const MaskedPrefix = "__masked__"

// MaskedValue returns "__masked__" for short secrets or "__masked__:…ab12cd" with the final six
// characters. Webhook identities usually occur at the end, while prefixes are shared. The suffix helps
// identify the bot without revealing the full credential.
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is an unchanged masked API response value.
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig copies configuration and masks the channel's credential fields. Unknown channel types
// return an empty map rather than risk exposing secrets. Preserve noncredential fields for display.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Treat nested structures such as headers as a single credential; per-subkey secret declarations would
		// add disproportionate complexity.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means the destination changed without explicit credential
// choices. See PrepareConfigUpdate for why silently retaining or dropping credentials is unsafe.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // Changed destination keys
	Missing []string // Credential keys without explicit choices
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "The destination (" + strings.Join(e.Changed, ", ") + ") changed. Reenter the credential fields (" +
		strings.Join(e.Missing, ", ") + ") as well: supply new values or leave them empty to indicate credentials are no longer needed. " +
		"Existing credentials are valid only for the previous address; reusing them would disclose them to the new address."
}

// PrepareConfigUpdate merges configuration while guarding destination changes. Plain MergeConfig
// retains omitted credentials, so changing only webhook.url, telegram.base_url, or email.host could
// send stored Authorization headers, bot tokens, or SMTP credentials to an attacker. This is silent
// and needs no redirect, defeating browser masking. Whenever a destination key changes, every
// credential key must explicitly contain a new value or an empty string to clear it. Omitted or masked
// values are rejected because they retain credentials valid only for the old destination. Do not
// silently drop optional credentials: a successful update with broken authentication is harder to
// diagnose than an actionable error.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("Unregistered channel type: %q", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// Reject masked sentinels nested in nonstring credentials (e.g. headers objects). MergeConfig
	// recognizes only top-level masked strings, so nested sentinels would be stored literally and silently
	// break authentication. Check this first, before the unchanged-destination early return, to cover all
	// updates.
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find destination keys whose values really changed; masked values mean unchanged.
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// With no destination change, merge normally: preserve masked values, clear empty strings, overwrite
		// other values.
		return MergeConfig(stored, incoming), nil
	}

	// The destination changed: require an explicit choice for every credential key.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects sentinels nested in nonstring structures. Masking applies to the
// entire value: an object such as headers must be submitted completely or replaced by a masked string.
// Nested sentinels cannot express preservation and would be stored as real values.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("Field %s contains a masking marker (%q). Omit the entire field to preserve the existing value or submit a complete new value; masking markers cannot be embedded inside structures",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares JSON encodings to tolerate equivalent numeric types (e.g. int versus
// float64). Normalize blank strings and absent keys first: MergeConfig deletes blank values, so they
// represent the same state. Otherwise an optional blank Telegram base_url disappears after the first
// save, appears changed on the second save, and rejects masked credentials until the user repastes a
// token despite changing nothing.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue must use the same clearing rule as MergeConfig (strings.TrimSpace(s) == "") so
// merge and comparison agree.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig overlays incoming configuration on stored values. Masked values retain originals; empty
// strings explicitly delete keys; other supplied values replace originals; omitted keys remain
// unchanged. Form blanks intentionally mean clear because the UI needs a way to remove a previously
// configured value.
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // Masked value means unchanged; preserve stored
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
