package usagecache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This file pins a TRUST STATEMENT rather than a mechanism, which is why it is
// its own file: design.md §7.24 says in words that a program able to write
// this directory is trusted exactly as far as one able to POST to the relay
// listener, and that the file write is in fact the STRONGER of the two. That
// sentence is the whole reason `telltale otel grok` carries no shared secret,
// so it must fail here if it ever stops being true — a later session that adds
// a bearer token to the listener should see this test and understand that the
// token buys nothing against this principal.
//
// Measured 2026-08-16 (§7.24) against the shipped binary with a redirected
// home: a hand-written grok.json claiming 4242 requests and 111111111 input
// tokens was read back as a live total, and a subsequent relayed request
// accumulated onto it and kept the hand-written `since`.

// A plain file write plants a complete, live entry. No relay, no listener, no
// parser — just a program writing the file, which is what every local program
// running as the operator can do.
func TestAHandWrittenEntryIsALiveTotal(t *testing.T) {
	dir := t.TempDir()
	planted := Entry{
		Vendor:          "grok",
		Since:           pinned.Add(-6 * time.Hour),
		WrittenAt:       pinned,
		Requests:        4242,
		InputTokens:     111111111,
		OutputTokens:    222222222,
		CacheReadTokens: 333333333,
		ReasoningTokens: ip(444444444),
	}
	raw, err := json.Marshal(planted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "grok.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	totals := ReadAll(dir, pinned)
	if len(totals) != 1 {
		t.Fatalf("ReadAll returned %d totals, want 1", len(totals))
	}
	got := totals[0]
	if got.Vendor != "grok" || got.Requests != 4242 || got.InputTokens != 111111111 {
		t.Fatalf("a hand-written entry did not read back whole: %+v", got.Entry)
	}
	if got.Span() != 6*time.Hour {
		t.Errorf("span = %v, want 6h — the writer chose the window, not the relay", got.Span())
	}
}

// The stronger half of the same statement, and the one that decides the
// design: the file writer sets fields the POST path CANNOT. A relayed request
// may only add four non-negative counts to whatever window is open; the file
// writer picks the window's start, its request count and every total outright.
// A secret on the HTTP path would leave all of that untouched.
func TestTheFileWriterSetsWhatTheRelayCannot(t *testing.T) {
	dir := t.TempDir()
	planted := Entry{
		Vendor:          "grok",
		Since:           pinned.Add(-6 * time.Hour),
		WrittenAt:       pinned,
		Requests:        4242,
		InputTokens:     111111111,
		OutputTokens:    222222222,
		CacheReadTokens: 333333333,
		ReasoningTokens: ip(444444444),
	}
	raw, _ := json.Marshal(planted)
	if err := os.WriteFile(filepath.Join(dir, "grok.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// One relayed request, the smallest possible, onto the planted window.
	if err := Add(dir, "grok", req(1, 1, 1, 1), pinned.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	totals := ReadAll(dir, pinned.Add(time.Minute))
	if len(totals) != 1 {
		t.Fatalf("ReadAll returned %d totals, want 1", len(totals))
	}
	got := totals[0]
	if got.Requests != 4243 || got.InputTokens != 111111112 {
		t.Fatalf("the relay did not accumulate onto the planted window: %+v", got.Entry)
	}
	if !got.Since.Equal(planted.Since) {
		t.Fatalf("since = %v, want the planted %v — the file writer owns the window start",
			got.Since, planted.Since)
	}
}
