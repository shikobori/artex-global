package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// These tests cover SSRF prevention against local/cloud-metadata targets and credential-free URL validation
// errors. Most package tests use localhost httptest receivers, so TestMain enables AllowLocalTargetsEnv;
// SSRF cases explicitly clear it to verify default rejection.

func TestMain(m *testing.M) {
	// Allow normal tests to reach local fake receivers; SSRF cases temporarily clear this setting.
	_ = os.Setenv(AllowLocalTargetsEnv, "1")
	os.Exit(m.Run())
}

// TestDialGuardRejectsLoopbackByDefault checks that the connection layer rejects loopback delivery by
// default.
func TestDialGuardRejectsLoopbackByDefault(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "") // Disable opt-in to test default behavior
	_, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}})
	if err == nil {
		t.Fatal("Loopback delivery should be blocked by default")
	}
	if hit {
		t.Fatal("Request reached the local service; guard did not work")
	}
	// The error should explain explicit opt-in, since local SMTP relays are legitimate.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("Rejection should explain explicit opt-in: %v", err)
	}
}

// TestDialGuardAllowsLoopbackWhenOptedIn preserves explicitly enabled local postfix and relay deployments.
func TestDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (dingTalkChannel{}).Send(context.Background(),
		map[string]any{"webhook": srv.URL + "/robot/send"}, Message{Items: []Item{{Severity: "high"}}}); err != nil {
		t.Fatalf("Delivery should succeed after opt-in: %v", err)
	}
}

func TestIsBlockedDialIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"169.254.169.254", // Cloud metadata endpoint, the main reason for this guard
		"169.254.1.1", "fe80::1",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"::ffff:127.0.0.1", // Normalize IPv4-mapped addresses before checking to prevent bypass
		"",
	}
	for _, s := range blocked {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be rejected", s)
		}
	}
	// RFC1918 destinations are deliberately allowed for self-hosted Mattermost and SMTP relays. This assertion
	// forces a conscious decision before changing policy rather than silently breaking existing deployments.
	allowed := []string{"10.0.0.5", "172.16.3.4", "192.168.1.10", "8.8.8.8", "2606:4700::1111"}
	for _, s := range allowed {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (private networks are legitimate delivery targets)", s)
		}
	}
}

// TestValidateHTTPURLRejectsLiteralPrivateTargets checks early validation: forbidden literal IPs should
// fail on save rather than first delivery.
func TestValidateHTTPURLRejectsLiteralPrivateTargets(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "")
	for _, raw := range []string{
		"http://127.0.0.1:8080/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:8080/hook",
	} {
		if err := validateHTTPURL(raw); err == nil {
			t.Errorf("%s should be rejected during configuration", raw)
		}
	}
	// Public and private addresses still pass; the dial guard also permits private networks.
	for _, raw := range []string{"https://oapi.dingtalk.com/robot/send", "http://10.0.0.9/hook"} {
		if err := validateHTTPURL(raw); err != nil {
			t.Errorf("%s should pass validation: %v", raw, err)
		}
	}
}

// TestValidateHTTPURLErrorNeverLeaksCredentials covers a previously missed parse-error path. A failing
// url.Parse returns *url.Error containing the original URL. Earlier file/gopher/ftp tests parsed
// successfully and exercised only scheme rejection, so passing them did not establish safety for parse
// failures.
func TestValidateHTTPURLErrorNeverLeaksCredentials(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/%zz?access_token=" + leakProbeToken,         // Invalid percent escape
		"https://a.example.com:port/x?access_token=" + leakProbeToken, // Nonnumeric port
		"http://[::1?access_token=" + leakProbeToken,                  // Unmatched bracket
	}
	for _, raw := range cases {
		// First verify that url.Parse actually fails; otherwise the test could silently exercise the wrong branch
		// and provide false assurance.
		if _, err := url.Parse(raw); err == nil {
			t.Errorf("%q must fail parsing or this test does not cover the intended branch", raw)
			continue
		}
		err := validateHTTPURL(raw)
		if err == nil {
			t.Errorf("%q should fail validation", raw)
			continue
		}
		assertNoSecret(t, err.Error(), leakProbeToken)
	}
	// The channel wrapper must not reintroduce the address.
	t.Setenv(AllowLocalTargetsEnv, "")
	err := (dingTalkChannel{}).Validate(map[string]any{"webhook": cases[0]})
	if err == nil {
		t.Fatal("Invalid address should fail validation")
	}
	assertNoSecret(t, err.Error(), leakProbeToken)
}

// TestEmailDialGuardRejectsLoopbackByDefault covers SMTP's dial guard. The old bare net.Dialer allowed
// loopback/metadata connections; handshake errors could expose server response lines through last_error,
// and refusal/timeout timing could probe ports. TestMain enables local targets for fake receivers, so this
// test must clear that opt-in or it passes even without a guard.
func TestEmailDialGuardRejectsLoopbackByDefault(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "") // Disable opt-in to test default behavior
	_, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil {
		t.Fatal("Email delivery to loopback should be blocked by default")
	}
	// The Control hook should block connection establishment before EHLO can be sent.
	if f.sawCommand("EHLO") || f.sawCommand("HELO") {
		t.Fatal("SMTP session established; guard did not work")
	}
	// The rejection must explain opt-in for legitimate local postfix relays.
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("Rejection should explain explicit opt-in: %v", err)
	}
}

// TestEmailDialGuardAllowsLoopbackWhenOptedIn verifies successful delivery after explicit opt-in for common
// self-hosted SMTP/local relay deployments.
func TestEmailDialGuardAllowsLoopbackWhenOptedIn(t *testing.T) {
	f := newFakeSMTP(t)
	cfg := emailCfg(t, f, nil)

	t.Setenv(AllowLocalTargetsEnv, "1")
	if _, err := (emailChannel{}).Send(context.Background(), cfg, singleMsg()); err != nil {
		t.Fatalf("Local SMTP delivery should succeed after opt-in: %v", err)
	}
	if !f.sawCommand("EHLO") {
		t.Fatal("No EHLO seen; session was not established")
	}
}
