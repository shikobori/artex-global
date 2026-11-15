package server

import (
	"context"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// Regression tests for English transport validation errors from connectMCP.
// Missing stdio commands, missing http/sse URLs, and unknown transports are
// rejected before dialing, so these paths need no database or network.
// The shared assertEnglishError assertion checks English source messages. Transport
// enums and URLs keep their wire spelling.
func TestConnectMCPErrorsLocalized(t *testing.T) {
	// Check the constants directly.
	for _, c := range []struct{ label, msg string }{
		{"stdio_no_command", errMCPStdioNoCommand},
		{"http_no_url", errMCPHTTPNoURL},
		{"sse_no_url", errMCPSSENoURL},
		{"unknown_transport", errMCPUnknownTransportFmt},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Exercise all four rejection paths; each returns before dialing.
	for _, c := range []struct {
		label string
		srv   *db.MCPServer
	}{
		{"stdio_missing_command", &db.MCPServer{Transport: "stdio", Command: ""}},
		{"http_missing_url", &db.MCPServer{Transport: "http", URL: ""}},
		{"sse_missing_url", &db.MCPServer{Transport: "sse", URL: ""}},
		{"unknown_transport_path", &db.MCPServer{Transport: "websocket"}},
	} {
		_, err := connectMCP(context.Background(), c.srv)
		if err == nil {
			t.Fatalf("%s: no error (validation passed)", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}
}
