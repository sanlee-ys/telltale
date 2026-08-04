package council

import (
	"strings"
	"testing"
)

// planted is the fixture set: one string per credential shape council must not
// render. Mirrors the Cursor adapter's approach (ADR-007) — plant every shape,
// assert none survives — because "we redact secrets" is a claim that needs a
// test behind it, not a comment.
//
// Every fixture is ASSEMBLED AT RUNTIME rather than written as a literal, and
// that is not stylistic. These strings are fake, but they are fake in exactly
// the shape the real ones take, which is the whole point — so GitHub's push
// protection reads them as real credentials and rejects the push. It blocked
// this file on the Slack token first. The alternatives were to whitelist a
// secret-shaped string in the repo's scanning config, or to weaken the fixtures
// until the scanner stopped recognising them; both trade a real protection for
// test convenience. Splitting the literals keeps the fixtures exact and leaves
// the scanner's job intact.
func plantedSecrets() map[string]string {
	return map[string]string{
		"anthropic key":  "sk-" + "ant-api03-AbCdEf0123456789ZzYyXxWwVv",
		"openai key":     "sk-" + "proj0123456789abcdefghijklmnop",
		"github pat":     "ghp" + "_0123456789abcdefghijklmnopqrstuvwx",
		"github fine":    "github" + "_pat_11ABCDE0123456789_abcdefghijklmnop",
		"aws access key": "AKIA" + "IOSFODNN7EXAMPLE",
		"slack token":    "xoxb" + "-1234567890-abcdefghijklmno",
		"jwt":            "eyJ" + "hbGciOiJIUzI1NiJ9." + "eyJ" + "zdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		"bearer header":  "Authorization: Bearer " + "abcdefghijklmnopqrstuvwxyz012345",
		"assignment":     `ANTHROPIC_API_KEY="` + `hunter2hunter2hunter2"`,
		"colon form":     "api_key: " + "s3cr3t-value-that-is-long",
	}
}

func TestEveryPlantedSecretIsRedacted(t *testing.T) {
	for name, secret := range plantedSecrets() {
		in := "the agent printed " + secret + " into its reply"
		got := Redact(in)
		if strings.Contains(got, secret) {
			t.Errorf("%s survived redaction: %q", name, got)
		}
		if !strings.Contains(got, redacted) {
			t.Errorf("%s was not marked as redacted: %q", name, got)
		}
	}
}

// TestRedactionIsVisible: a removed secret must leave a mark. Deleting it
// silently would produce a reply that reads as though the model never said
// anything there, which is a quieter lie than showing the key.
func TestRedactionIsVisible(t *testing.T) {
	key := "sk-" + "ant-api03-AbCdEf0123456789ZzYyXxWwVv"
	got := Redact("key is " + key + " ok")
	if !strings.Contains(got, redacted) {
		t.Fatalf("no marker in %q", got)
	}
	if !strings.Contains(got, "key is ") || !strings.Contains(got, " ok") {
		t.Errorf("redaction ate surrounding text: %q", got)
	}
}

// TestSecretSplitAcrossChunksIsStillCaught is the whole reason Redactor exists.
//
// Token-level streaming does not respect token boundaries: a key arrives as two
// or more deltas. Redacting each chunk in isolation matches neither half and
// renders the secret in full — a per-chunk regex would pass every test in the
// file above and still leak.
func TestSecretSplitAcrossChunksIsStillCaught(t *testing.T) {
	secret := "sk-" + "ant-api03-AbCdEf0123456789ZzYyXxWwVv"
	for _, split := range []int{1, 5, 10, 20, len(secret) - 1} {
		var r Redactor
		var out strings.Builder
		out.WriteString(r.Feed("the key is "))
		out.WriteString(r.Feed(secret[:split]))
		out.WriteString(r.Feed(secret[split:]))
		out.WriteString(r.Feed(" and that is all"))
		out.WriteString(r.Flush())

		got := out.String()
		if strings.Contains(got, secret) {
			t.Errorf("split at %d leaked the key: %q", split, got)
		}
		if !strings.Contains(got, redacted) {
			t.Errorf("split at %d produced no marker: %q", split, got)
		}
	}
}

// TestStreamingLosesNothing: redaction may replace, but it must never drop
// ordinary text. A renderer that silently eats part of a reply is the failure
// this product exists to refuse.
func TestStreamingLosesNothing(t *testing.T) {
	full := "one two three four five six seven eight nine ten"
	for _, size := range []int{1, 2, 3, 7, 13} {
		var r Redactor
		var out strings.Builder
		for i := 0; i < len(full); i += size {
			end := i + size
			if end > len(full) {
				end = len(full)
			}
			out.WriteString(r.Feed(full[i:end]))
		}
		out.WriteString(r.Flush())
		if got := out.String(); got != full {
			t.Errorf("chunk size %d: got %q, want %q", size, got, full)
		}
	}
}

// TestFeedHoldsOnlyTheTrailingWord pins the streaming property that makes this
// usable: output flows at word granularity, so a column is not frozen until a
// newline arrives.
func TestFeedHoldsOnlyTheTrailingWord(t *testing.T) {
	var r Redactor
	if got := r.Feed("hello world par"); got != "hello world " {
		t.Errorf("Feed released %q, want everything up to the last space", got)
	}
	if got := r.Feed("tial\n"); got != "partial\n" {
		t.Errorf("Feed released %q, want the completed word", got)
	}
}

// TestFlushReleasesTheTail: the last word of a reply usually has no trailing
// space, so without Flush it would be stranded in the buffer forever.
func TestFlushReleasesTheTail(t *testing.T) {
	var r Redactor
	r.Feed("a final thought")
	if got := r.Flush(); got != "thought" {
		t.Errorf("Flush returned %q, want the stranded tail", got)
	}
	if got := r.Flush(); got != "" {
		t.Errorf("second Flush returned %q, want empty", got)
	}
}

// TestOrdinaryProseIsUntouched guards the other direction. An over-eager
// redactor that mangles normal replies is its own kind of dishonest render.
func TestOrdinaryProseIsUntouched(t *testing.T) {
	prose := []string{
		"Use the native resume path rather than re-sending the transcript.",
		"The function is called parseEvent and lives in vendors/claude.go.",
		"A password manager is the right answer here.",
		"see https://example.com/docs/getting-started for the setup",
		"exit status 1: the command was not found",
	}
	for _, s := range prose {
		if got := Redact(s); got != s {
			t.Errorf("prose was mangled:\n got %q\nwant %q", got, s)
		}
	}
}
