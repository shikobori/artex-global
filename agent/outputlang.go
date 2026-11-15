package agent

// User-facing output language selection. artex-global requires agents to write
// user-visible natural language in English, Korean, Chinese, Spanish, or Japanese.
// This setting controls the language of user-facing output independently of the
// reasoning prompt body in section [A].

// OutputLanguage is wired by the server to return the output language code
// ("en"/"ko"/"zh"/"es"/"ja") selected through config ARTEX_LANG and runtime settings KV.
// A nil hook or empty value uses the fallback below. Like PromptOverride, the
// server injects this hook.
var OutputLanguage func() string

// Default output language when the server has not wired the hook (for example,
// isolated agent package tests). The fallback is English, matching the production
// configuration default. Other supported languages remain selectable.
const defaultOutputLang = "en"

// SupportedOutputLangs lists the selectable output language codes.
var SupportedOutputLangs = []string{"en", "ko", "zh", "es", "ja"}

// normalizeLang maps unsupported codes to the default.
func normalizeLang(code string) string {
	for _, c := range SupportedOutputLangs {
		if c == code {
			return code
		}
	}
	return defaultOutputLang
}

// resolveOutputLang returns the current output language code, preferring the
// server-provided hook and otherwise using the default.
func resolveOutputLang() string {
	if OutputLanguage != nil {
		if code := OutputLanguage(); code != "" {
			return normalizeLang(code)
		}
	}
	return defaultOutputLang
}

// langPhrase names the language to use in instructions. Native names in
// parentheses disambiguate the selected language for the model.
var langPhrase = map[string]string{
	"ko": "Korean (한국어)",
	"en": "English",
	"zh": "Chinese (简体中文)",
	"es": "Spanish (Español)",
	"ja": "Japanese (日本語)",
}

// langBare provides short language names for repeated references in instructions.
var langBare = map[string]string{
	"ko": "Korean",
	"en": "English",
	"zh": "Chinese",
	"es": "Spanish",
	"ja": "Japanese",
}
