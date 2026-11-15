package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/db"
)

// containsNonEnglishLetter detects non-Latin letters in generated English text.
func containsNonEnglishLetter(s string) (rune, bool) {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return r, true
		}
	}
	return 0, false
}

func sampleFindingNode(t *testing.T) *db.Node {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"vulnclass": "SQL Injection",
		"name":      "Login form SQL injection",
		"severity":  "high",
		"summary":   "Error-based SQL injection was confirmed in the login parameter.",
		"evidence":  map[string]string{"poc": "' OR '1'='1"},
	})
	if err != nil {
		t.Fatalf("Failed to serialize payload: %v", err)
	}
	return &db.Node{ID: 1, Kind: "finding", Payload: payload}
}

func sampleDBFinding() *db.DBFinding {
	return &db.DBFinding{
		ID:              123,
		VulnClass:       "SQL Injection",
		Name:            "Login form SQL injection",
		Severity:        "high",
		Summary:         "Error-based SQL injection was confirmed in the login parameter.",
		Evidence:        "' OR '1'='1",
		Status:          "confirmed",
		Report:          "Detailed analysis.",
		CreatedAt:       time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		EvidenceVersion: 2,
		TaskDescription: "Demo target penetration test",
		TrafficBindings: []db.FindingTrafficBinding{
			{
				ID:   9,
				Role: "request",
				Note: "Injection payload delivery point",
				Snapshot: db.TrafficEvidenceSnapshot{
					Method: "POST",
					URL:    "https://sandbox.local/login",
					Status: 200,
				},
			},
		},
	}
}

// TestReportMarkdownEnglish verifies the exploration report uses English headings.
func TestReportMarkdownEnglish(t *testing.T) {
	out := Markdown(Input{
		Title:       "Demo task",
		Goal:        "Comprehensive sandbox review",
		GeneratedAt: time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC),
		AssetCounts: map[string]int{"host": 2, "url": 5},
		Findings:    []*db.Node{sampleFindingNode(t)},
	})
	if r, ok := containsNonEnglishLetter(out); ok {
		t.Fatalf("Report scaffold contains a non-English letter %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Penetration test report: Demo task",
		"- **Task goal**: Comprehensive sandbox review",
		"- **Generated at**:",
		"## Summary",
		"- Confirmed vulnerabilities: **1**",
		"## Vulnerabilities",
		"**PoC / Evidence:**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Report scaffold is missing %q", want)
		}
	}
}

// TestReportMarkdownEmptyEnglish verifies the empty-state message is English.
func TestReportMarkdownEmptyEnglish(t *testing.T) {
	out := Markdown(Input{Title: "Empty task", GeneratedAt: time.Now()})
	if r, ok := containsNonEnglishLetter(out); ok {
		t.Fatalf("Empty report contains a non-English letter %q:\n%s", string(r), out)
	}
	if !strings.Contains(out, "_No vulnerabilities were confirmed in this run._") {
		t.Errorf("Empty report message is not English:\n%s", out)
	}
}

// TestFindingsMarkdownEnglish verifies English summary/detail headings and severity labels.
func TestFindingsMarkdownEnglish(t *testing.T) {
	f := sampleDBFinding()
	out := FindingsMarkdown([]*db.DBFinding{f}, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsNonEnglishLetter(out); ok {
		t.Fatalf("Findings summary contains a non-English letter %q:\n%s", string(r), out)
	}
	for _, want := range []string{
		"# Vulnerability summary report",
		"- **Generated at**:",
		"- **Total vulnerabilities**: 1",
		"## Summary",
		"| Severity | Count |",
		"| Critical |", "| High |", "| Medium |", "| Low |", // Severity labels match UI status.severity.
		"## Vulnerability details",
		"- **Class**: SQL Injection",
		"- **Status**: confirmed",
		"- **Task**: Demo target penetration test",
		"- **Discovered at**:",
		"**Evidence:**",
		"**Detailed report:**",
		"## Related traffic evidence",
		"Evidence version: 2, binding count: 1.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Summary report is missing %q", want)
		}
	}

	single := SingleFindingMarkdown(f, time.Date(2026, 10, 4, 2, 30, 0, 0, time.UTC))
	if r, ok := containsNonEnglishLetter(single); ok {
		t.Fatalf("Single finding report contains a non-English letter %q:\n%s", string(r), single)
	}
	for _, want := range []string{
		"- **Severity**: high",
		"## Overview",
		"## Evidence",
		"## Detailed report",
		"[Request message](evidence/123/9/request.http)",
		"[Response message](evidence/123/9/response.http)",
	} {
		if !strings.Contains(single, want) {
			t.Errorf("Single finding report is missing %q", want)
		}
	}
}

// TestFindingsCSVEnglish verifies English CSV headers after the BOM; data values are preserved.
func TestFindingsCSVEnglish(t *testing.T) {
	raw := FindingsCSV([]*db.DBFinding{sampleDBFinding()})
	out := strings.TrimPrefix(string(raw), "\xEF\xBB\xBF")
	header := strings.SplitN(out, "\n", 2)[0]
	if r, ok := containsNonEnglishLetter(header); ok {
		t.Fatalf("CSV header contains a non-English letter %q: %s", string(r), header)
	}
	for _, col := range []string{"ID", "Name", "Class", "Severity", "Status", "Task", "Discovered at", "Overview", "Traffic evidence count", "Traffic evidence IDs"} {
		if !strings.Contains(header, col) {
			t.Errorf("CSV header is missing column %q: %s", col, header)
		}
	}
}
