package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements DingTalk custom bots. Bots are limited to 20 messages/minute; excess
// messages may be silently dropped even with HTTP 200, so rate limiting is client-side
// (DefaultRatePerMin). Security supports signing, keywords, or IP allowlists; this adapter supports
// signing, which does not depend on message content, and unsigned webhooks. Check the response errcode
// even on HTTP 200.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// The DingTalk Webhook URL contains access_token and must be masked in full.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// Changing the DingTalk Webhook destination requires explicitly specifying the signing secret again.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return errors.New("Webhook URL is missing")
	}
	if err := validateHTTPURL(hook); err != nil {
		return fmt.Errorf("Invalid Webhook URL: %w", err)
	}
	return nil
}

// Send delivers one message, using ActionCard for a single item with a detail link, and markdown
// otherwise.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk has no explicit markdown byte limit; still bound it to guard against oversized evidence
	// fields.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    "View details",
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk reports application errors inside HTTP 200 responses.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, fmt.Errorf("Failed to parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 means invalid signature; 310000 means keyword mismatch. Both are configuration errors that
		// retries cannot fix.
		return 0, Permanent(fmt.Errorf("DingTalk error (%d): %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL adds timestamp and sign using DingTalk's signing rules. Sign timestamp + "\n" +
// secret with HMAC-SHA256 using secret as the key, then base64 and URL encode the result. The
// timestamp is in milliseconds. An empty secret leaves the URL unchanged for unsigned bots.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not return err directly: url.Parse errors contain the full URL, including access_token.
		return "", fmt.Errorf("Failed to parse Webhook URL: %s", redactRequestTarget(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL validates URL syntax, supported schemes, and literal IP destinations. Errors must be
// redacted because url.Parse errors include the full URL and embedded credentials; otherwise
// credentials leak into API errors, last_error, logs, and delivery history. Literal IPs are checked
// here for immediate configuration feedback; hostnames are checked by blockInternalDial at connection
// time to cover DNS rebinding. Restricting schemes also excludes unexpected file/gopher behavior.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("Unable to parse URL (%s)", redactRequestTarget(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Only http or https is supported; got: %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("Hostname is missing")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return fmt.Errorf("Delivery to local or link-local address %s is not allowed (set %s=1 to deliver to local services)", ip, AllowLocalTargetsEnv)
	}
	return nil
}
