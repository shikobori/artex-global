// Package selfupdate implements one-click updates in ARTEX: download a new binary
// from GitHub Releases, verify and stage it, then atomically replace it at the next startup.
//
// Responsibilities (see start.sh / start.bat):
//
// Startup scripts = simple supervisor loops that decide whether to restart based on exit codes.
// This package = error-prone logic (download / SHA256 verification / smoke test / replacement / rollback).
//
// Replacement lives in Go because shell and batch scripts would need separate checksum
// and smoke-test implementations (sha256sum / shasum / certutil). A non-runnable binary
// would restart forever under the supervisor and require manual recovery on the host.
//
// A complete upgrade takes three process launches:
//
// 1. Old server receives /api/update/apply -> download/verify -> stage artex.new -> exit 75.
// 2. Supervisor restarts old binary -> Bootstrap finds artex.new -> verify/smoke-test -> replace -> exit 75.
// 3. Supervisor starts new binary -> Bootstrap counts an attempt -> clear marker after stable startup.
//
// Failures retain or restore the old version: step 2 discards an invalid staged binary;
// step 3 automatically restores artex.old after three startups fail to reach marker cleanup.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart asks the supervisor to restart (EX_TEMPFAIL) immediately without crash backoff.
// Exit 0 is a normal user stop (end the loop); all other codes count as crashes.
const ExitRestart = 75

// maxAttempts limits post-replacement starts. Each new-version startup increments the count;
// surviving settleDelay clears the marker, while maxAttempts crashes trigger automatic rollback.
const maxAttempts = 3

// Paths lists upgrade files, all located in the executable directory.
// Do not use CWD: service working directories may be / or arbitrary paths, which would
// stage files elsewhere and break the replacement logic.
type Paths struct {
	Dir     string // Executable directory
	Current string // Currently running binary: artex / artex.exe
	New     string // Staged new version: artex.new / artex.new.exe
	Sum     string // New version SHA256 (hex): artex.new.sha256 / artex.new.exe.sha256
	Old     string // Old version backup: artex.old / artex.old.exe
	Marker  string // Upgrade state marker: artex.upgrade.json
}

// ResolvePaths derives all upgrade paths from the current executable.
//
// On Windows, .new/.old must retain the .exe suffix for smoke tests and execution.
// Strip the extension first, then append it again to keep naming symmetric across platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("Failed to locate the executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // ".exe" on Windows, usually empty on Unix
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker records replacement progress to trigger automatic rollback if the new version cannot start.
type marker struct {
	From     string `json:"from"`     // Version before upgrade
	To       string `json:"to"`       // Target version
	Attempts int    `json:"attempts"` // Startup attempts after replacement
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes staged files after replacement, failed verification, or cancellation
// so a leftover artex.new is not retried on the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares version numbers, returning -1/0/1 (a<b / a==b / a>b).
// ok=false means at least one version is not comparable, such as a local "dev" build or
// git describe output "0.3.7-2-gabc1234-dirty". Disable one-click updates in that case
// to avoid replacing a development build and its uncommitted changes with a release.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses "v0.3.7" / "0.3.7" version strings into [3]int.
//
// Accept only clean three-part versions. For non-tag builds, build.sh uses git describe
// to produce suffixes such as "0.3.7-2-gabc1234"; mark these incomparable, not 0.3.7,
// or development builds may be misreported as current or overwritten with a release.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports whether the process runs in a container. Replacement writes to its
// writable layer; recreating it with `docker compose up -d` restores the image version.
// This is expected when pulling a new image, but the frontend must explain it.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
