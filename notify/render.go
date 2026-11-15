package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes limits s to max bytes without splitting UTF-8 characters. WeCom allows 4096 bytes;
// splitting a three-byte Chinese character would cause rejection or garbled text. Backtrack from the budget
// to the nearest rune start using utf8.RuneStart. max<=0 means unlimited. Append an ellipsis unless max
// cannot fit it.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// If max is shorter than the ellipsis, omit it rather than exceeding the limit.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses whitespace and truncates by characters for IM titles. Uncollapsed summary newlines
// would break table/title layouts. max<=0 means unlimited.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes limits s to max characters, appending an ellipsis when truncated; max<=0 means unlimited.
// WeCom limits bytes, while Telegram limits characters. Keep both functions: the wrong measure silently
// reduces Chinese output to roughly one third of the intended size.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML limits HTML by characters without leaving partial tags. Truncating to a fragment such as <a
// href="htt can cause rejection or consume following text as an attribute. Truncate first, then backtrack
// before an unclosed <. Do not balance tags: Telegram closes them automatically, and handling quoted
// attributes, comments, and self-closing tags adds disproportionate complexity.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// If the final < has no following >, backtrack before it.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// Also backtrack before a partial entity, such as &amp; cut to &amp. An incomplete entity can cause the
	// entire oversized notification to be rejected.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount counts complete items that fit a digest. Rendering then truncating would silently lose
// later findings while their records reported success; whole-item packing leaves omitted items for the next
// batch and returns the true delivered count. maxSize<=0 is unlimited; reserve budgets headers/footers;
// size uses channel-specific bytes (WeCom/DingTalk) or characters (Telegram); render measures the actual
// item text rather than estimating. Return at least one for nonempty input and let final truncation handle
// an oversized item, so it cannot permanently block the batch.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize / runeSize name the two packItemCount measurement units so callers are clearer than anonymous
// length functions.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine renders assets on one line, omitting entries beyond limit while stating the total. Listing
// dozens of assets for one finding would crowd out the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + " (" + itoa(len(assets)) + " total)"
}

// itoa provides a short integer-to-string conversion for display text without importing strconv throughout
// the package.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
