package llmpool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/Autumn-27/norma/llm"
)

// hasNonEnglishLetters reports whether s contains letters outside the Latin script.
func hasNonEnglishLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

// assertSurfacedExhaustion checks the error an exhausted chain hands back: it must
// still wrap the ErrExhausted sentinel (callers rely on errors.Is), read as an
// English message (no non-Latin letters, no full-width colon), and lead with the
// English sentinel text. failProv's body is ASCII ("anthropic: status 402:
// nope"), so the whole surfaced string must be free of non-Latin letters.
func assertSurfacedExhaustion(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: exhausted chain returned nil error", label)
	}
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("%s: surfaced error no longer wraps ErrExhausted: %v", label, err)
	}
	msg := err.Error()
	if hasNonEnglishLetters(msg) {
		t.Errorf("%s: surfaced error still carries non-Latin letters: %q", label, msg)
	}
	if strings.ContainsRune(msg, '：') {
		t.Errorf("%s: surfaced error still uses a full-width colon: %q", label, msg)
	}
	if !strings.HasPrefix(msg, ErrExhausted.Error()) {
		t.Errorf("%s: surfaced error does not lead with the English sentinel: %q", label, msg)
	}
}

// The chain-exhaustion error is user-facing: a worker whose whole LLM chain fails
// records it through agent/capture.go as a "result" activity shown in the run
// transcript. So its text must be English, while its identity (errors.Is) must be
// preserved for callers that branch on the sentinel.
func TestExhaustedErrorEnglish(t *testing.T) {
	assertEnglishErrText(t, "ErrExhausted", ErrExhausted.Error())

	// Drive both surfaced paths so a future edit to either wrap site is caught.
	streamChain := New([]*Member{
		member(1, "a", 10, failProv("a", 402)),
		member(2, "b", 5, failProv("b", 402)),
	}, NewRegistry(nil, nil))
	_, serr := drain(streamChain.Stream(context.Background(), llm.CompletionRequest{}))
	assertSurfacedExhaustion(t, "Stream", serr)

	completeChain := New([]*Member{
		member(1, "a", 10, failProv("a", 402)),
		member(2, "b", 5, failProv("b", 402)),
	}, NewRegistry(nil, nil))
	_, _, _, cerr := completeChain.Complete(context.Background(), llm.CompletionRequest{})
	assertSurfacedExhaustion(t, "Complete", cerr)
}

// assertEnglishErrText pins the English text of the exhaustion sentinel.
func assertEnglishErrText(t *testing.T, label, s string) {
	t.Helper()
	if s != "LLM fallback chain: no available configurations" {
		t.Errorf("%s: unexpected English exhaustion message: %q", label, s)
	}
}
