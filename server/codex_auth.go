package server

import (
	"net/http"
	"os/exec"

	"github.com/Autumn-27/artex/codexauth"
)

func (s *Server) codexStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := exec.LookPath("codex"); err != nil {
		writeJSON(w, 200, map[string]any{"connected": false, "error": "codex CLI is not on the ARTEX server's PATH"})
		return
	}
	if err := codexauth.Available(); err != nil {
		writeJSON(w, 200, map[string]any{"connected": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"connected": true})
}
