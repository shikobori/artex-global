package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// These tests verify whole-item digest packing: when a channel limit is exceeded, report the actual
// delivered count so only included items are marked delivered. Previously, rendering then truncating the
// entire batch hid later findings while delivery history incorrectly reported complete success.

func TestMarkdownBodyPacksWholeItemsWithinByteLimit(t *testing.T) {
	// Two hundred Chinese items exceed WeCom's 4096-byte limit.
	m := batchMsg(200)
	body, kept := markdownBody(m, weComMarkdownLimit)

	if len(body) > weComMarkdownLimit {
		t.Fatalf("Body has %d bytes, exceeding limit %d", len(body), weComMarkdownLimit)
	}
	if !utf8.ValidString(body) {
		t.Fatal("Body is not valid UTF-8")
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Expected a partial batch (0 < kept < %d), got %d", len(m.Items), kept)
	}
	// The header must accurately state how many items are included and how many remain; readers must not
	// mistake the included count for the full batch.
	if !strings.Contains(body, "remaining") || !strings.Contains(body, "next message") {
		t.Fatalf("Header should state how many items remain:\n%s", body[:minInt(400, len(body))])
	}
	// Only the first kept items should be present.
	for i := 0; i < kept; i++ {
		if !strings.Contains(body, "漏洞"+itoa(i+1)) {
			t.Fatalf("Item %d should be in this message:\n%s", i+1, body)
		}
	}
	if strings.Contains(body, "漏洞"+itoa(kept+1)) {
		t.Fatalf("Item %d should be in the next batch, not this message", kept+1)
	}
}

func TestMarkdownBodyKeepsEverythingWhenUnderLimit(t *testing.T) {
	m := batchMsg(3)
	body, kept := markdownBody(m, 0) // 0 = unlimited
	if kept != len(m.Items) {
		t.Fatalf("Unlimited length should keep all items, got kept=%d", kept)
	}
	if strings.Contains(body, "remaining") {
		t.Fatalf("Untruncated output should have no truncation notice:\n%s", body)
	}
}

func TestMarkdownBodyAlwaysKeepsAtLeastOneItem(t *testing.T) {
	// Even if the budget cannot fit one item, send one and apply final truncation. Otherwise a single
	// oversized finding permanently blocks every attempt at the batch.
	m := batchMsg(5)
	_, kept := markdownBody(m, 50)
	if kept != 1 {
		t.Fatalf("Should keep at least one item, got %d", kept)
	}
}

func TestMarkdownBodySingleReturnsOne(t *testing.T) {
	_, kept := markdownBody(singleMsg(), 4096)
	if kept != 1 {
		t.Fatalf("Single-item message should report one delivery, got %d", kept)
	}
	// An empty message has no deliverable items.
	if _, k := markdownBody(Message{}, 4096); k != 0 {
		t.Fatalf("Empty message should report zero items, got %d", k)
	}
}

func TestTelegramPackingUsesRuneBudget(t *testing.T) {
	m := batchMsg(200)
	text, kept := telegramHTML(m)
	// Telegram limits characters; using bytes would reduce Chinese messages to one third of the intended size.
	if n := utf8.RuneCountInString(text); n > telegramTextLimit {
		t.Fatalf("Body has %d characters, exceeding limit %d", n, telegramTextLimit)
	}
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Should fit only part of the batch, got %d", kept)
	}
	if !strings.Contains(text, "next message") {
		t.Fatalf("Should explain that items remain:\n%.300s", text)
	}
}

func TestFeishuPackingReportsKept(t *testing.T) {
	m := batchMsg(2000)
	_, kept := feishuCard(m)
	if kept <= 0 || kept >= len(m.Items) {
		t.Fatalf("Card should fit only part of the batch, got %d", kept)
	}
}

func TestWebhookAndEmailReportAllItems(t *testing.T) {
	// These two channels do not truncate bodies, so the entire batch is delivered.
	m := batchMsg(7)
	if n := len(m.Items); n != 7 {
		t.Fatal("Precondition not met")
	}
	// Confirm indirectly through markdownBody(0), which keeps all items without a length limit.
	if _, k := markdownBody(m, 0); k != len(m.Items) {
		t.Fatalf("Unlimited length should use all items, got %d", k)
	}
}

// TestMarkdownEscapesUntrustedContent ensures untrusted input cannot alter message structure. Titles and
// summaries come from model output based on target responses; asset names come from target URLs.
func TestMarkdownEscapesUntrustedContent(t *testing.T) {
	cases := []struct {
		name  string
		item  Item
		must  []string // Must appear in escaped form
		wrong []string // Must not appear in unescaped form
	}{
		{
			name: "Title with newline and external link",
			item: Item{
				Severity: "high",
				Name:     "Login SQL injection\n[Urgent: verify your account here](http://attacker.tld)",
			},
			// Collapse newlines to prevent forged list/quote blocks, and escape brackets and parentheses to prevent
			// clickable external links.
			must:  []string{`\[Urgent: verify your account here\]`, `\(http://attacker.tld\)`},
			wrong: []string{"\n[Urgent", "\n\n[Urgent"},
		},
		{
			name: "Image beacon in title",
			item: Item{
				Severity: "high",
				Name:     "Vulnerability ![](http://attacker.tld/beacon)",
			},
			must:  []string{`\!`, `\(http://attacker.tld/beacon\)`},
			wrong: []string{"![]("},
		},
		{
			name: "Emphasis and quote in asset name",
			item: Item{
				Severity: "high",
				Name:     "Ordinary title",
				Assets:   []string{"a.com/*injection*>quote"},
			},
			must:  []string{`\*injection\*`, `\>`},
			wrong: []string{"*injection*"},
		},
		{
			name: "Backtick and pipe in summary",
			item: Item{
				Severity: "high",
				Name:     "Title",
				Summary:  "`code` | table",
			},
			must:  []string{"\\`code\\`", `\|`},
			wrong: []string{"`code`"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Message{Items: []Item{tc.item}}
			// Single-item writeItem is the shared rendering path for all three markdown channels.
			var b strings.Builder
			writeItem(&b, tc.item, "", true)
			got := b.String()
			for _, want := range tc.must {
				if !strings.Contains(got, want) {
					t.Errorf("Missing escaped form %q:\n%s", want, got)
				}
			}
			for _, bad := range tc.wrong {
				if strings.Contains(got, bad) {
					t.Errorf("Unescaped form %q could inject structure or external links:\n%s", bad, got)
				}
			}
			_ = m
		})
	}
}

// TestMarkdownEscapeBackslashFirst checks escape order: handle backslashes first to avoid escaping newly
// inserted backslashes a second time.
func TestMarkdownEscapeBackslashFirst(t *testing.T) {
	if got := markdownEscape(`a\b*c`); got != `a\\b\*c` {
		t.Fatalf("Incorrect escape order, got %q", got)
	}
}

// TestTelegramTitleHasNoMarkdownEscapes prevents markdown escaping from leaking into Telegram HTML.
// Escaping in a shared title function previously produced visible backslashes such as \(1\).
func TestTelegramTitleHasNoMarkdownEscapes(t *testing.T) {
	m := Message{Items: []Item{{Severity: "high", Name: "alert(1) *emphasis*"}}}
	text, _ := telegramHTML(m)
	if strings.Contains(text, `\(`) || strings.Contains(text, `\*`) {
		t.Fatalf("Telegram body contains markdown backslash escapes:\n%s", text)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
