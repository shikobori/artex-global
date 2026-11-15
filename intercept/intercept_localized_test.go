package intercept

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

// assertEnglish rejects empty text or non-Latin letters in English messages.
func assertEnglish(t *testing.T, label, s string) {
	t.Helper()
	if strings.TrimSpace(s) == "" {
		t.Errorf("%s: empty message", label)
	}
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			t.Errorf("%s: non-English letter in message: %q", label, s)
			break
		}
	}
}

// TestInterceptMessagesEnglish pins the user-facing judge/approval messages to
// English. These surface in the approval record reason, the activity stream, and
// the 409 response for an already-decided request.
func TestInterceptMessagesEnglish(t *testing.T) {
	for _, c := range []struct{ name, s string }{
		{"msgReviewContextIncomplete", msgReviewContextIncomplete},
		{"msgModelApprovalFailed", msgModelApprovalFailed},
		{"msgModelOutputUnparsable", msgModelOutputUnparsable},
		{"reasonWorkCanceled", reasonWorkCanceled},
		{"reasonWorkCanceledPreExec", reasonWorkCanceledPreExec},
		{"reasonApprovalTimeout", reasonApprovalTimeout},
		{"reasonManualDeny", reasonManualDeny},
		{"reasonManualAllow", reasonManualAllow},
		{"ErrAlreadyDecided", ErrAlreadyDecided.Error()},
	} {
		assertEnglish(t, c.name, c.s)
	}
}

// TestJudgeActionLabelEnglish checks the three judge verdict labels are English
// (allow / deny / ask) and that an unknown action still
// passes through untranslated.
func TestJudgeActionLabelEnglish(t *testing.T) {
	for _, action := range []string{"allow", "deny", "ask"} {
		assertEnglish(t, "judgeActionLabel("+action+")", judgeActionLabel(action))
	}
	if got := judgeActionLabel("weird"); got != "weird" {
		t.Errorf("judgeActionLabel(weird) = %q, want passthrough", got)
	}
}

// TestDefaultMessageEnglish checks the rule-derived deny/ask messages are English
// and still embed the rule name, while allow stays empty.
func TestDefaultMessageEnglish(t *testing.T) {
	for _, action := range []string{"deny", "ask"} {
		msg := defaultMessage(action, "R1")
		assertEnglish(t, "defaultMessage("+action+")", msg)
		if !strings.Contains(msg, "R1") {
			t.Errorf("defaultMessage(%s) dropped rule name: %q", action, msg)
		}
	}
	if got := defaultMessage("allow", "R1"); got != "" {
		t.Errorf("defaultMessage(allow) = %q, want empty", got)
	}
}

// TestToolApprovalSummaryEnglish checks the activity summary is English and stays
// parseable by transcript.tsx, which extracts the pending id via /\(#(\d+)\)/ and
// the tool name via /Tool\s+(\S+)\s+approval/.
func TestToolApprovalSummaryEnglish(t *testing.T) {
	s := fmt.Sprintf(msgToolApprovalRequestFmt, "Bash", 42)
	assertEnglish(t, "msgToolApprovalRequestFmt", s)
	if !strings.Contains(s, "(#42)") {
		t.Errorf("summary lost the (#N) marker (transcript.tsx pending_id regex): %q", s)
	}
	if !strings.Contains(s, "Tool Bash approval") {
		t.Errorf("summary lost the 'Tool X approval' shape (transcript.tsx toolName regex): %q", s)
	}
}
