package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	// Check the key UTF-8 invariant at every possible cut using mixed Chinese/English inputs of varied
	// lengths. WeCom limits bytes, and splitting a three-byte Chinese character would produce invalid UTF-8
	// and rejection.
	inputs := []string{
		"中文测试内容",
		"混合 mixed 内容 content",
		"a中b文c测d试e",
		"🔴🟠🟡🔵", // Four-byte emoji makes invalid cuts more obvious
		strings.Repeat("漏洞", 100),
	}
	for _, in := range inputs {
		for max := 1; max <= len(in)+2; max++ {
			got := TruncateBytes(in, max)
			if !utf8.ValidString(got) {
				t.Fatalf("Input %q max=%d: invalid UTF-8 output %q", in, max, got)
			}
			if len(got) > max {
				t.Fatalf("Input %q max=%d: output of %d bytes exceeds limit", in, max, len(got))
			}
			// Do not change content that fits the limit.
			if len(in) <= max && got != in {
				t.Fatalf("Input %q max=%d: content within limit was changed -> %q", in, max, got)
			}
		}
	}
}

func TestTruncateBytesZeroMeansUnlimited(t *testing.T) {
	long := strings.Repeat("x", 10000)
	if got := TruncateBytes(long, 0); got != long {
		t.Fatal("max=0 should mean unlimited")
	}
	if got := TruncateBytes(long, -5); got != long {
		t.Fatal("max<0 should mean unlimited")
	}
}

func TestTruncateBytesEllipsisBudget(t *testing.T) {
	// When max cannot fit an ellipsis, adding one must not exceed the limit.
	got := TruncateBytes("abcdefgh", 1)
	if len(got) > 1 {
		t.Fatalf("max=1: output %q of length %d exceeds limit", got, len(got))
	}
	// Ordinary truncation should include an ellipsis.
	if got := TruncateBytes("abcdefgh", 5); !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("Expected ellipsis, got %q", got)
	}
}

func TestTruncateRunesCountsCharactersNotBytes(t *testing.T) {
	// Preserve the difference from TruncateBytes: Telegram counts characters, and byte limits would reduce
	// Chinese text to one third of the intended length.
	s := "一二三四五六七八九十"
	got := TruncateRunes(s, 5)
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("Expected five characters, got %d (%q)", n, got)
	}
	// The same string should be much shorter when measured in bytes.
	if utf8.RuneCountInString(TruncateBytes(s, 5)) >= 5 {
		t.Fatal("Byte and character limits should produce different character counts")
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	got := OneLine("第一行\n\n第二行\t带制表   多空格", 0)
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("Should collapse all whitespace, got %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("Should not retain consecutive spaces, got %q", got)
	}
	// Truncated output must remain readable and valid.
	got = OneLine("一二三四五六七八九十", 4)
	if n := utf8.RuneCountInString(got); n != 4 {
		t.Fatalf("Expected four characters, got %d (%q)", n, got)
	}
}

func TestTruncateHTMLNeverCutsTagInHalf(t *testing.T) {
	// Naive HTML truncation can leave <a href="htt and cause the entire message to be rejected.
	s := `<b>标题</b>正文正文正文<a href="https://example.com/very/long/path">查看详情</a>`
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		if n := utf8.RuneCountInString(got); max > 0 && n > max {
			t.Fatalf("max=%d: output of %d characters exceeds limit", max, n)
		}
		// The tail must not contain an unclosed < without a following >.
		if lt := strings.LastIndex(got, "<"); lt >= 0 && !strings.Contains(got[lt:], ">") {
			t.Fatalf("max=%d: trailing tag was cut -> %q", max, got)
		}
	}
}

func TestAssetLineOmitsExcess(t *testing.T) {
	if got := assetLine(nil, 3); got != "" {
		t.Fatalf("No assets should return an empty string, got %q", got)
	}
	if got := assetLine([]string{"a", "b"}, 3); got != "a, b" {
		t.Fatalf("Should list all assets within limit, got %q", got)
	}
	// State the total when assets are omitted so readers know how many remain.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	if !strings.Contains(got, "(5 total)") {
		t.Fatalf("Should state a total of five, got %q", got)
	}
}

func TestSeverityAndStatusLabels(t *testing.T) {
	if AtLeast("", "low") {
		t.Fatal("Empty severity ranks zero and should fail any threshold")
	}
	if !AtLeast("critical", "") {
		t.Fatal("Empty threshold should allow all")
	}
	if got := StatusLabel("fixed"); got != "Fixed" {
		t.Fatalf("Incorrect status mapping, got %q", got)
	}
	// Return unknown statuses unchanged instead of inventing labels.
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Fatalf("Unknown status should be unchanged, got %q", got)
	}
}

// TestTruncateHTMLNeverCutsEntity covers the audit finding that truncation must avoid partial HTML entities
// as well as tags. Cutting &amp; to &amp can cause an entire long digest to be rejected.
func TestTruncateHTMLNeverCutsEntity(t *testing.T) {
	s := "aaaa&amp;bbbb&lt;cccc&quot;dddd"
	for max := 1; max <= utf8.RuneCountInString(s)+2; max++ {
		got := TruncateHTML(s, max)
		// Do not leave a trailing & without its matching semicolon.
		if amp := strings.LastIndex(got, "&"); amp >= 0 && !strings.Contains(got[amp:], ";") {
			t.Fatalf("max=%d: trailing partial entity %q", max, got[amp:])
		}
		if strings.Contains(got, "&amp\x00") {
			t.Fatalf("max=%d: malformed entity", max)
		}
	}
}
