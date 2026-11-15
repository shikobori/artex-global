package notify

import (
	"strings"
	"testing"
)

// hanFreeItems creates ASCII titles, types, and summaries so language assertions detect renderer labels
// rather than fixture content.
func hanFreeItems(n int) []Item {
	items := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, Item{
			FindingID: int64(i + 1),
			Name:      "login-flaw",
			VulnClass: "SQLi",
			Severity:  "high",
			Summary:   "SQL injection via q param",
			Assets:    []string{"a.example.com"},
		})
	}
	return items
}

// TestMarkdownTitleLocalized checks the English default title shared by markdown channels, Feishu, HTML,
// Telegram, and Webhook.
func TestMarkdownTitleLocalized(t *testing.T) {
	// Batch title: "Vulnerability summary · N total".
	got := markdownTitle(Message{Batch: true, Items: hanFreeItems(3)})
	assertEnglish(t, "markdownTitle(batch)", got)
	if !strings.Contains(got, "Vulnerability summary") || !strings.Contains(got, "3 total") {
		t.Errorf("Expected batch title 'Vulnerability summary · 3 total', got %q", got)
	}
	// No items: "Vulnerability notification".
	empty := markdownTitle(Message{})
	assertEnglish(t, "markdownTitle(empty)", empty)
	if empty != "Vulnerability notification" {
		t.Errorf("Empty message title should be 'Vulnerability notification', got %q", empty)
	}
}

// TestMarkdownBatchIntroLocalized checks English digest intros, including time windows, counts, and
// overflow notices. ASCII fixtures isolate the renderer labels.
func TestMarkdownBatchIntroLocalized(t *testing.T) {
	items := hanFreeItems(3)

	// With a time window: new vulnerabilities in the last N minutes.
	win := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 3)
	if hasHan(win) {
		t.Errorf("Time-window intro contains CJK characters: %q", win)
	}
	if !strings.Contains(win, "Last 30 minutes") || !strings.Contains(win, "3 new vulnerabilities") {
		t.Errorf("Expected intro 'Last 30 minutes: 3 new vulnerabilities', got %q", win)
	}

	// Without a time window: only the new vulnerability count.
	noWin := markdownBatchIntro(Message{Batch: true}, items, 3)
	if hasHan(noWin) {
		t.Errorf("Intro contains CJK characters: %q", noWin)
	}
	if !strings.Contains(noWin, "3 new vulnerabilities") || strings.Contains(noWin, "minutes") {
		t.Errorf("Intro without time window should say '3 new vulnerabilities' without minutes, got %q", noWin)
	}

	// Partial batch: show the included count and explain that remaining items continue in the next message.
	trunc := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 5)
	if hasHan(trunc) {
		t.Errorf("Overflow notice contains CJK characters: %q", trunc)
	}
	for _, want := range []string{"showing first 3", "remaining 2", "next message"} {
		if !strings.Contains(trunc, want) {
			t.Errorf("Overflow notice should contain %q, got %q", want, trunc)
		}
	}
}

// TestWriteItemLabelsLocalized checks English status, type, asset, and summary labels and the details link
// in the shared single-item markdown renderer.
func TestWriteItemLabelsLocalized(t *testing.T) {
	it := Item{
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/1",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
	var b strings.Builder
	writeItem(&b, it, "", true)
	got := b.String()

	assertEnglish(t, "writeItem(single)", got)
	for _, want := range []string{
		"**Status change**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Summary**: SQL injection via q param",
		"[View details](https://platform.example/finding/1)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Single-item output should contain %q:\n%s", want, got)
		}
	}
}

// TestMarkdownBodyFooterLocalized checks the English platform link appended to digest bodies when HomeURL
// is present.
func TestMarkdownBodyFooterLocalized(t *testing.T) {
	m := Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)}
	body, kept := markdownBody(m, 0)
	if kept != 2 {
		t.Fatalf("Unlimited (0) should include both items, got %d", kept)
	}
	if !strings.Contains(body, "[View all on the platform](https://platform.example)") {
		t.Errorf("Body should end with a 'View all on the platform' link:\n%s", body)
	}
}
