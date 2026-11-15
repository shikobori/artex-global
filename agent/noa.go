package agent

import (
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is the model-driven context-compaction mechanism introduced in norma v0.4.0, exposed as an
// experimental platform setting. It is mutually exclusive with built-in compaction: noaadapter.Enable
// is the sole entry point, installing the Compactor, Compress tool, and three persistent prompt blocks.
// Without Enable, built-in compaction continues normally. Each agent resolves its injected noaEnabledFn
// once per run, so toggling affects only subsequent runs without rebuilding the agent.

// enableNoa attaches noa to opts when the resolver reports it enabled. archiveRoot is the persistent
// base directory for source text (global workDir, consistently under <workDir>/noa rather than scattered
// across task/intent directories); globally unique sessionID names the archive subdirectory.
//
// noa is experimental: setup failure must not interrupt real tasks. Report via onWarn and fall back to built-in compaction.
// On success, clear opts.Compaction to avoid the agentcore warning about two context managers.
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn("Failed to enable noa compaction; falling back to built-in compaction: " + err.Error())
		}
		return
	}
	// Compactor overrides Compaction, but agentcore warns every time both are present; explicitly clear it.
	opts.Compaction = nil
}
