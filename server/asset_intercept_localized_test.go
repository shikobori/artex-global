package server

import "testing"

// Regression tests for English errors from the asset and task interception rule
// validators. Both validators are pure functions with no database access, so these
// tests check constants and actual error paths. The shared assertEnglishError helper
// checks English messages. Wire identifiers remain unchanged.
func TestAssetInterceptErrorsLocalized(t *testing.T) {
	// Pin the constants.
	for _, c := range []struct{ label, msg string }{
		{"pattern_empty", errAssetInterceptPatternEmpty},
		{"bad_exact_ip", errAssetInterceptInvalidExactIPFmt},
		{"bad_cidr", errAssetInterceptInvalidCIDRFmt},
		{"bad_kind", errAssetInterceptInvalidKindFmt},
		{"bad_action", errTaskInterceptInvalidAction},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Exercise all four rejection branches of validateAssetInterceptRuleReq.
	for _, c := range []struct {
		label string
		req   assetInterceptRuleReq
	}{
		{"empty_pattern", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "   "}},
		{"invalid_exact_ip", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "not-an-ip"}},
		{"invalid_cidr", assetInterceptRuleReq{Kind: "cidr", Pattern: "nonsense"}},
		{"invalid_kind", assetInterceptRuleReq{Kind: "nope", Pattern: "example.com"}},
	} {
		req := c.req
		err := validateAssetInterceptRuleReq(&req)
		if err == nil {
			t.Fatalf("%s: no error (validation passed)", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}

	// Valid input must be accepted.
	okReq := assetInterceptRuleReq{Kind: "cidr", Pattern: "192.168.0.0/16"}
	if err := validateAssetInterceptRuleReq(&okReq); err != nil {
		t.Fatalf("valid CIDR was rejected: %v", err)
	}

	// Exercise the action rejection branch of validateTaskInterceptRuleReq.
	badAction := taskInterceptRuleReq{Action: "nope", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&badAction); err == nil {
		t.Fatalf("bad_action_path: no error (validation passed)")
	} else {
		assertEnglishError(t, "bad_action_path", err.Error())
	}

	// An empty action is normalized to the default, block.
	defaultAction := taskInterceptRuleReq{Action: "", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&defaultAction); err != nil {
		t.Fatalf("failed to normalize the default action: %v", err)
	}
	if defaultAction.Action != "block" {
		t.Fatalf("default action was not normalized to block: %q", defaultAction.Action)
	}
}
