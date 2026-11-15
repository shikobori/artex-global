package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/config"
)

// TestTrMsgSwitchesLanguage verifies that English source messages follow the
// selected output language, including the standalone English fallback.
func TestTrMsgSwitchesLanguage(t *testing.T) {
	previousLanguage := config.CurrentLanguage
	t.Cleanup(func() { config.CurrentLanguage = previousLanguage })

	const source = errAssetIDRequired
	assertEnglishError(t, "source message", source)
	translations, ok := errCatalog[source]
	if !ok {
		t.Fatalf("source message is missing from the catalog: %q", source)
	}

	for _, lang := range []string{"en", "ko", "zh", "es", "ja"} {
		config.CurrentLanguage = func() string { return lang }
		if got := trMsg(source); got != translations[lang] || got == "" {
			t.Fatalf("%s: trMsg = %q, want %q", lang, got, translations[lang])
		}
		if lang == "en" && trMsg(source) != source {
			t.Fatalf("en: trMsg must preserve the English source")
		}
		if got := trMsg(translations["ko"]); got != translations[lang] {
			t.Fatalf("%s: legacy Korean lookup = %q, want %q", lang, got, translations[lang])
		}
	}

	config.CurrentLanguage = nil
	if got := trMsg(source); got != source {
		t.Fatalf("unwired: trMsg = %q, want English fallback %q", got, source)
	}
	config.CurrentLanguage = func() string { return "fr" }
	if got := trMsg(source); got != source {
		t.Fatalf("unknown language: trMsg = %q, want English fallback %q", got, source)
	}

	config.CurrentLanguage = func() string { return "en" }
	const notInCatalog = "some-lower-layer-error: connection refused"
	if got := trMsg(notInCatalog); got != notInCatalog {
		t.Fatalf("non-catalog string must pass through unchanged, got %q", got)
	}
}

// Legacy messages with the same English source retain their original localized
// wording even when the canonical catalog combines those English keys.
func TestTrMsgLegacyVariants(t *testing.T) {
	previousLanguage := config.CurrentLanguage
	t.Cleanup(func() { config.CurrentLanguage = previousLanguage })
	for legacy, translations := range legacyErrCatalog {
		for _, lang := range []string{"en", "ko", "zh", "es", "ja"} {
			config.CurrentLanguage = func() string { return lang }
			if got := trMsg(legacy); got != translations[lang] || got == "" {
				t.Errorf("legacy %q [%s] = %q, want %q", legacy, lang, got, translations[lang])
			}
		}
	}
}

// The status template must use the same newline and quote bytes as its runtime
// constant, rather than the literal escape sequences emitted by the old catalog.
func TestTrMsgFallbackChatStatus(t *testing.T) {
	previousLanguage := config.CurrentLanguage
	t.Cleanup(func() { config.CurrentLanguage = previousLanguage })
	translations, ok := errCatalog[fallbackChatStatus]
	if !ok {
		t.Fatal("fallback chat status template is missing from the catalog")
	}
	escapeLegacy := strings.NewReplacer("\n", `\n`, `"`, `\"`)
	for _, lang := range []string{"en", "ko", "zh", "es", "ja"} {
		config.CurrentLanguage = func() string { return lang }
		template := trMsg(fallbackChatStatus)
		if template == "" || template != translations[lang] {
			t.Fatalf("%s: fallback template = %q, want %q", lang, template, translations[lang])
		}
		if lang == "en" && template != fallbackChatStatus {
			t.Fatalf("en: fallback template must match the English source: %q", template)
		}
		if lang != "en" && template == fallbackChatStatus {
			t.Fatalf("%s: fallback template was not translated", lang)
		}
		formatted := fmt.Sprintf(template, 3, 2, 1)
		if strings.Count(formatted, "\n") != 1 || strings.Contains(formatted, `\`) || strings.Contains(formatted, "%!") {
			t.Fatalf("%s: malformed fallback status: %q", lang, formatted)
		}
		for _, source := range []string{fallbackChatStatus, translations["ko"]} {
			if got := trMsg(escapeLegacy.Replace(source)); got != template {
				t.Fatalf("%s: legacy escaped template = %q, want %q", lang, got, template)
			}
		}
	}
}
