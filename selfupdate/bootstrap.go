package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv makes a child launched by the smoke test skip Bootstrap.
//
// Without it, the child executable is artex.new and all derived paths have a .new
// prefix, so they cannot touch the real upgrade files. Relying on that coincidence
// is fragile; an explicit short circuit is clearer and avoids unnecessary disk probes.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action is the instruction Bootstrap returns to main.
type Action int

const (
	// Continue: start the server normally.
	Continue Action = iota
	// Restart: exit immediately with ExitRestart so the supervisor restarts the process.
	Restart
)

// State describes the upgrade state at this startup so /api/update/check can tell
// the frontend whether the previous upgrade succeeded or rolled back.
type State struct {
	Pending     bool   // Replacement has not yet been confirmed stable
	RolledBack  bool   // Automatic rollback just ran during this startup
	FailedStage bool   // Staged binary failed verification/smoke testing and was discarded
	Detail      string // A one-line user-facing explanation
}

// Bootstrap runs at the very beginning of main, before listening on ports or opening databases.
//
// Three possible states:
//
// 1. artex.new exists: verify and smoke-test; replace and restart on success, discard and run the old version on failure.
// 2. Only the marker remains: replacement completed; count an attempt and roll back after enough failures.
// 3. Neither exists: normal startup.
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf("[update] skipping bootstrap: %v", err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged handles a staged binary: replace it after verification, or discard it on failure.
//
// This is the only place in the upgrade flow that replaces the executable, and the
// last gate: smoke testing catches corruption, wrong architecture, and missing dynamic
// libraries. A non-runnable binary would restart forever without reaching Go rollback code.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf("[update] staged version failed verification, discarded; continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "The new version failed verification and was discarded: " + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf("[update] version swap failed, continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "Failed to replace the version: " + err.Error()}
	}

	// Replacement succeeded. Keep the marker for the next startup of the new version to confirm stability.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to write upgrade marker (automatic rollback unavailable): %v", err)
	}
	log.Printf("[update] swapped to %s, exiting to restart (exit %d)", orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback counts post-replacement startup attempts and restores the old version after the limit.
//
// The counter increments only once Go code runs, covering binaries that execute but
// crash during initialization (incompatible config, occupied ports, failed DB migrations).
// The pre-replacement smoke test covers exec failures; together they cover both cases.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If rollback also fails, do not restart again and enter an infinite loop. Clear the
			// marker and start with the current state so startup errors are at least visible in logs.
			log.Printf("[update] new version failed to start %d times in a row and rollback failed: %v", maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: "The new version failed to start, and rollback also failed: " + err.Error()}
		}
		log.Printf("[update] new version failed to start %d times in a row, rolled back to %s, exiting to restart (exit %d)",
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf("The new version failed to start; rolled back to the previous version (%s)", orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to update upgrade marker: %v", err)
	}
	log.Printf("[update] starting new version (attempt %d/%d), upgrade is confirmed once it runs stably",
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle confirms that the new version has run stably and clears the upgrade marker.
//
// main calls this after a delay once HTTP is listening. If the process does not survive
// that long, leave the marker so subsequent startups count attempts until rollback.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // Not a post-upgrade startup; nothing to do
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[update] failed to clear upgrade marker: %v", err)
		return
	}
	log.Printf("[update] new version is stable, upgrade complete (previous version kept as %s)", p.Old)
}

// SettleDelay is how long the new version must run before being considered stable.
const SettleDelay = 30 * time.Second

// verifyStaged checks SHA256, then actually launches the staged binary once.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return fmt.Errorf("Failed to read checksum: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return fmt.Errorf("Failed to calculate checksum: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return errors.New("SHA256 mismatch (the download is corrupted or has been tampered with)")
	}
	return smokeTest(p.New)
}

// smokeTest launches the new binary with -h to verify it can execute on this system.
// This catches truncated downloads, wrong architecture (exec format error), and missing dependencies.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return fmt.Errorf("Failed to grant execute permission: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return errors.New("Smoke test timed out (the new binary did not respond)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("Smoke test failed: %v: %s", err, snippet)
	}
	return nil
}

// swap replaces the current binary with the staged new version.
//
// Unix and Windows both allow renaming a running executable (Windows forbids deleting
// or overwriting it, not renaming), so no platform split or prior self-shutdown is needed.
func swap(p Paths) error {
	// Windows rename does not overwrite a destination; remove .old left by the previous upgrade first.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Failed to remove previous backup %s: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return fmt.Errorf("Failed to back up the current version: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// Replacement failed after moving the current binary; restore it or the next startup has no executable.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return fmt.Errorf("Failed to install the new version (%v), and failed to restore the current version: %w", err, rerr)
		}
		return fmt.Errorf("Failed to install the new version: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback restores the old version backed up by swap.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return fmt.Errorf("No backup is available for rollback (%s): %w", p.Old, err)
	}
	// Move the non-starting version to .failed for troubleshooting instead of deleting it.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return fmt.Errorf("Failed to move the failed version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return fmt.Errorf("Failed to restore the previous version: %w", err)
	}
	return nil
}

// Rollback implements /api/update/rollback: explicitly return to the previous version.
// It only swaps binaries; the supervisor handles restart after the caller exits with ExitRestart.
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New("No previous version is available for rollback (" + p.Old + " is missing)")
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return fmt.Errorf("Refusing rollback because the previous version cannot run: %w", err)
	}
	// Exchange current and backup so the rollback can itself be reversed.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return fmt.Errorf("Failed to move the current version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return fmt.Errorf("Failed to install the previous version: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf("[update] failed to tidy backups after rollback (no runtime impact): %v", err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup reports whether a previous version is available so the frontend can show the rollback button.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown version"
	}
	return s
}
