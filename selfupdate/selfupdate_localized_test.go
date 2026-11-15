package selfupdate

// Regression tests for English user-facing self-update progress and errors.
// updateHub surfaces Stage callbacks as SSE progress.message and returned errors
// from Stage/FetchLatest/Rollback as progress.error, writeErr bodies, or boot_notice.
// Keep these messages English throughout the update card.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode"
)

// assertEnglishError requires a nonempty error with Latin letters and no letters
// from other scripts. Wrapped standard-library errors remain valid.
func assertEnglishError(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", label)
	}
	msg := err.Error()
	hasLatin := false
	for _, r := range msg {
		if unicode.IsLetter(r) {
			if !unicode.Is(unicode.Latin, r) {
				t.Errorf("%s: non-English letters remain: %q", label, msg)
				return
			}
			hasLatin = true
		}
	}
	if !hasLatin {
		t.Errorf("%s: expected an English error message: %q", label, msg)
	}
}

// rtFunc injects fake http.Client responses without connecting to GitHub.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubClient(status int, body string) *http.Client {
	return &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

// TestCheckURLErrorsEnglish checks the language of both URL rejection paths.
func TestCheckURLErrorsEnglish(t *testing.T) {
	nonHTTPS := checkURL(mustParse(t, "http://api.github.com/x"))
	assertEnglishError(t, "non-https", nonHTTPS)
	if !strings.Contains(nonHTTPS.Error(), "HTTPS") {
		t.Errorf("non-https error does not mention HTTPS: %q", nonHTTPS.Error())
	}

	nonGitHub := checkURL(mustParse(t, "https://evil.example.com/x"))
	assertEnglishError(t, "non-github-host", nonGitHub)
	if !strings.Contains(nonGitHub.Error(), "GitHub") {
		t.Errorf("non-github error does not mention GitHub: %q", nonGitHub.Error())
	}
}

// TestFetchLatestErrorsEnglish checks rate limits, missing releases, other status
// codes, parsing failures, and missing tags through a fake transport without network access.
func TestFetchLatestErrorsEnglish(t *testing.T) {
	ctx := context.Background()

	_, errRate := FetchLatest(ctx, stubClient(http.StatusForbidden, ""))
	assertEnglishError(t, "rate-limited", errRate)
	if !strings.Contains(errRate.Error(), "60") {
		t.Errorf("Rate-limit error does not mention 60: %q", errRate.Error())
	}

	_, errNF := FetchLatest(ctx, stubClient(http.StatusNotFound, ""))
	assertEnglishError(t, "not-found", errNF)

	_, errStatus := FetchLatest(ctx, stubClient(http.StatusInternalServerError, ""))
	assertEnglishError(t, "bad-status", errStatus)
	if !strings.Contains(errStatus.Error(), "GitHub") {
		t.Errorf("Status error does not mention GitHub: %q", errStatus.Error())
	}

	_, errJSON := FetchLatest(ctx, stubClient(http.StatusOK, "{not json"))
	assertEnglishError(t, "bad-json", errJSON)

	_, errTag := FetchLatest(ctx, stubClient(http.StatusOK, "{}"))
	assertEnglishError(t, "missing-tag", errTag)
	if !strings.Contains(errTag.Error(), "tag") {
		t.Errorf("Missing-tag error does not mention tag: %q", errTag.Error())
	}
}

// TestStageMissingAssetEnglish checks the error before downloading a missing platform
// package. checkWritable may fail first depending on the environment; both errors
// must be English, and neither path accesses the network.
func TestStageMissingAssetEnglish(t *testing.T) {
	rel := &Release{TagName: "v9.9.9"} // No assets
	err := Stage(context.Background(), &http.Client{}, rel, "0.0.1", nil)
	assertEnglishError(t, "stage-missing-asset", err)
}
