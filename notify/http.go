package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets controls delivery to loopback/link-local addresses, denied by default. These
// ranges include local admin services and cloud metadata (169.254.169.254), whose responses could leak
// via doJSON error snippets and delivery history if a compromised/shared admin session changes
// configuration. Local SMTP relays are legitimate, so ARTEX_NOTIFY_ALLOW_LOCAL=1 explicitly opts in.
// Export AllowLocalTargetsEnv so tests using loopback httptest receivers can opt in too.
const AllowLocalTargetsEnv = "ARTEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP identifies destinations denied by default: loopback, link-local (including cloud
// metadata), unspecified, and multicast. RFC1918 private addresses remain allowed for legitimate
// self-hosted Mattermost and SMTP relays; this tradeoff protects sensitive destinations without
// breaking normal deployments.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Unmap IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1) before checking to prevent bypasses.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the transport dialer's Control hook, checking the address when the connection
// is established. This final enforcement point covers DNS rebinding after configuration validation and
// redirects, including same-host redirects that can change paths.
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("Unable to parse destination address: %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("Delivery to local or link-local address %s is not allowed (set %s=1 to deliver to local services)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport clones the default Transport and adds only a dial guard, preserving connection
// pooling, HTTP/2, timeouts, proxy settings, and other tuning.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is shared by all delivery channels. Do not reuse server GlobalProxy: that potentially
// unstable tunnel carries target traffic and should not affect notification availability. Use a
// 15-second timeout. Reject cross-host redirects because credentials are embedded in
// DingTalk/WeCom/Telegram URLs and would leak to the destination. Same-host redirects, such as adding
// a trailing slash, remain allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("Too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("Cross-host redirects are not allowed (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit bounds response reads. Delivery history needs only an error code and short
// description, even if a faulty peer returns excessive content.
const respBodyLimit = 8 << 10

// doJSON sends a request and returns a bounded response body. A nil payload sends an empty body;
// custom Webhook headers are attached as provided. Network failures and HTTP 5xx/408/429 are
// retryable; other 4xx responses are permanent, since retrying a 403 only repeats the same error.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// Serialization failure is a local configuration/type bug; retries will not fix it.
			return nil, Permanent(fmt.Errorf("Failed to build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// An invalid URL is a permanent configuration failure. Do not return url.Parse errors directly because
		// they contain the complete address.
		return nil, Permanent(fmt.Errorf("Invalid request URL: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refusal, DNS failure, and timeouts are usually transient and use backoff retries. Redact
		// errors before returning them: http.Client.Do returns *url.Error containing the full URL, including
		// embedded platform credentials. Otherwise those secrets reach plaintext last_error storage, delivery
		// history (bypassing configuration masking), server logs, and the test-send API's 502 response.
		return nil, fmt.Errorf("Request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("Failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// Retry 429 (rate limiting) and 408 (timeout); other 4xx errors indicate configuration or permission
	// problems.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("Remote server rate limited or timed out (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("Remote server error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("Remote server rejected request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet flattens and shortens response bodies for errors so multiline/whitespace-heavy text cannot
// break delivery-history layout.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget reduces addresses to scheme://host/… for errors. Discard everything except
// scheme and host: credentials occur in DingTalk/WeCom queries, Feishu's final path segment, and
// Telegram's middle path segment. Per-channel selective redaction is easy to get wrong. The host is
// enough for DNS, connection, and certificate troubleshooting; masked suffixes in channel
// configuration identify the bot. Parse failures return a fixed placeholder, never the raw input.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unable to parse URL)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError removes addresses while retaining the underlying cause. Read *url.Error.Err
// explicitly instead of Error(), which includes the URL. This avoids fragile replacement across URL
// encoding/escaping variants.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: Unknown error", uerr.Op, host)
	}
	// Other error types, including redirect-policy errors, may contain URLs too; redact them consistently.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) URLs in unstructured errors with redacted forms. Recognize
// HTTP/HTTPS prefixes and split on whitespace or quotes, which cannot occur literally in URLs.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
