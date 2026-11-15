package notify

import (
	"fmt"
	"strings"
)

// Shared markdown rendering for DingTalk and WeCom. Feishu cards, Telegram HTML, and email HTML are
// rendered by their own adapters.

// maxAssetsShown limits displayed assets; listing dozens would crowd messages with little value.
const maxAssetsShown = 3

// maxSummaryRunes limits the summary length. Notifications direct readers to details; the
// full report remains on the platform.
const maxSummaryRunes = 120

// markdownReservedBytes reserves room for the digest heading, severity counts, overflow notice, and
// platform link. Deduct this before packing items so readers can still identify the batch and omitted
// count.
const markdownReservedBytes = 320

// markdownEscape escapes structural markdown characters. Titles, summaries, classes, and asset names
// come from untrusted model/target output, including attacker-controlled URL queries. Unescaped links
// could become phishing links; image beacons could reveal readership and IPs; injected formatting
// could hide later findings. Escape heading/link/emphasis/list/quote/strikethrough characters,
// processing backslashes first to avoid escaping newly inserted ones.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText flattens untrusted text to one line and escapes it. Flattening also prevents
// newline-based fake list items and quotes, which character escaping alone cannot stop.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns an unescaped message/card title. It is shared across markdown, Telegram HTML,
// Feishu plain_text, Webhook JSON, and email subjects, whose escaping rules differ. Each output
// renderer must escape locally (writeItem, feishuItemLines, telegramEscape); shared markdown escaping
// previously left visible backslashes such as \(1\) in Telegram.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("Vulnerability summary · %d total", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "Vulnerability notification"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody returns the rendered body and count actually included. Callers must mark only the first
// kept items delivered and defer the rest; otherwise truncation silently loses items while delivery
// records claim success. maxBytes<=0 disables the limit.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// Send even an oversized single item, with final truncation as a fallback; partial finding information
		// is better than none.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[View all on the platform](%s)\n", m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false)
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false)
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro renders the time window, count, and severity distribution for quick triage. items
// contains the entries that fit; total is the full batch count. Explicitly state how many continue in
// the next message so omitted entries are not invisible.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**Last %d minutes: %d new vulnerabilities**", m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, "**%d new vulnerabilities**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, " (showing first %d, remaining %d continue in the next message)", len(items), extra)
	}
	// Count only included items by severity so the distribution matches the entries the reader can see.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders a finding. prefix numbers digest entries; single=true includes the summary and
// detail link, while digests use compact lines. All external title/class/asset/summary fields pass
// through markdownText. The detail link comes from administrator-configured public_base_url and is
// emitted as a clickable link.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest mode uses one line with shortened assets and summary.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**Status change**: %s → %s\n",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		fmt.Fprintf(b, "**Type**: %s\n", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		fmt.Fprintf(b, "**Assets**: %s\n", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, "**Summary**: %s\n", s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, "[View details](%s)\n", it.DetailURL)
	}
}
