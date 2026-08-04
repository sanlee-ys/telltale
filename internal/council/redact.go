package council

import (
	"regexp"
	"strings"
)

// redacted is what replaces a matched secret. Visible on purpose: silently
// deleting the text would leave a reply that reads as if the model never said
// anything there.
const redacted = "«redacted»"

// secretPatterns are the credential shapes council refuses to render.
//
// This is best-effort pattern matching and is documented as such — it is a
// second line of defence, not a guarantee. The first line is that council never
// reads a credential store: the vendors authenticate themselves, and telltale
// holds no tokens. What this catches is a secret that ends up in vendor OUTPUT,
// which is a real path (an agent greps a .env, or echoes a header it was
// debugging) and one the Cursor adapter already taught this repo to take
// seriously (ADR-007).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}`),
	// JWTs: three base64url segments, and the header always starts "eyJ".
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{20,}`),
	// Assignment form. The name is matched with optional surrounding word
	// characters rather than \b, because the real-world spelling is
	// ANTHROPIC_API_KEY, not api_key — and \b fails there, since the preceding
	// underscore is itself a word character. That near-miss is why this pattern
	// has a fixture of its own.
	regexp.MustCompile(`(?i)[a-z0-9_\-]*(api[_\-]?key|access[_\-]?token|auth[_\-]?token|secret|password|passwd)[a-z0-9_\-]*\s*[:=]\s*["']?[^\s"']{8,}`),
}

// Redact replaces every credential-shaped run in s.
func Redact(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}

// Redactor makes redaction safe over a STREAM.
//
// The problem it solves: text arrives in chunks that do not respect token
// boundaries, so a key can be split across two deltas ("sk-ant-abc" then
// "def123..."). Redacting each chunk as it lands would match neither half and
// render the secret in full.
//
// The fix is to release only up to the last whitespace. Every pattern above
// describes a run with no spaces in it, so a secret can never straddle a
// whitespace boundary — which makes the text before the last space safe to emit
// and the tail after it the only thing that must be held. That keeps output
// flowing at word granularity rather than waiting for a newline, and it needs no
// timer: the buffered tail is at most one word.
type Redactor struct {
	pending string
}

// Feed takes a raw chunk and returns the text that is now safe to display.
func (r *Redactor) Feed(chunk string) string {
	r.pending += chunk
	i := strings.LastIndexAny(r.pending, " \t\n\r")
	if i < 0 {
		// No boundary yet: the whole buffer could still be the front half of a
		// secret. Hold it.
		return ""
	}
	out := r.pending[:i+1]
	r.pending = r.pending[i+1:]
	return Redact(out)
}

// Flush releases whatever is still held. Called when a turn ends, so the final
// word of a reply is not stranded in the buffer.
func (r *Redactor) Flush() string {
	if r.pending == "" {
		return ""
	}
	out := Redact(r.pending)
	r.pending = ""
	return out
}
