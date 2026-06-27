// user_agent_test.go — TDD test pinning the User-Agent header contract.
//
// GitHub's REST API requires a User-Agent header. Requests without one
// get rate-limited more aggressively and may be blocked entirely.
// See: https://docs.github.com/en/rest/overview/resources-in-the-rest-api
//
// This test pins the contract: every outgoing Check() request must
// include a User-Agent header identifying awp.
package update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCheck_SendsUserAgentHeader verifies that the outgoing HTTP request
// to the GitHub releases API carries a User-Agent header identifying awp.
//
// CORRECT-7 self-check:
//   C-onformance: User-Agent header must start with "awp/"
//   O-rdering: N/A (single request)
//   R-ange: N/A
//   R-eference: requires httptest server
//   E-xistence: header must exist
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestCheck_SendsUserAgentHeader(t *testing.T) {
	var capturedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://x"}`))
	}))
	defer server.Close()

	checker := NewChecker("v0.1.0")
	checker.SetAPIBaseURL(server.URL)
	_ = checker.Check()

	if capturedUA == "" {
		t.Fatal("User-Agent header was empty; GitHub API requires it.")
	}
	if !strings.HasPrefix(capturedUA, "awp/") {
		t.Errorf("User-Agent = %q; want it to start with 'awp/' so GitHub can identify our requests.\n"+
			"Without a User-Agent, GitHub rate-limits or blocks the request.",
			capturedUA)
	}
}

// TestCheck_SendsAcceptHeader verifies the Accept header is set to
// GitHub's recommended vnd.github+json content type.
func TestCheck_SendsAcceptHeader(t *testing.T) {
	var capturedAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://x"}`))
	}))
	defer server.Close()

	checker := NewChecker("v0.1.0")
	checker.SetAPIBaseURL(server.URL)
	_ = checker.Check()

	if !strings.Contains(capturedAccept, "vnd.github") {
		t.Errorf("Accept = %q; want it to include 'vnd.github+json' for stable GitHub API contract.", capturedAccept)
	}
}