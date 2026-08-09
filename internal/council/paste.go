package council

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// maxPasteRunes is the most a paste may leave in the draft, measured in runes
// over the draft-plus-paste total.
//
// The number is anchored to the narrowest pipe a brief has to fit through: the
// Antigravity seat's prompt rides argv (`-p <prompt>`, vendors/agy.go — agy
// reads nothing from stdin), and a Windows command line caps at 32,767 UTF-16
// units. 8,192 runes is at most 16,384 units even if every rune costs a
// surrogate pair, which leaves the other half of the ceiling for the binary,
// the flags and the rebuttal quoting a dispatch may wrap around the draft. A
// paste that pushed the draft past that is not refused for the render's sake —
// wrap() would survive it — it is refused because one seat could no longer be
// HANDED the brief, and a composer that accepts text it cannot deliver is
// dishonest one keystroke later than this refusal is.
//
// It is also where the composer stops being an editor. The compose area shows
// at most maxComposerRows of the tail and deletes rune by rune, so a paste the
// size of a log file would be held somewhere the operator can neither read nor
// practically remove. The refusal names the remedy: long material goes in a
// file, and the brief names the path.
//
// The cap is paste-shaped on purpose: typing can still pass it, as it always
// could, because nobody types eight thousand characters into a footer by
// accident and the hazard this constant answers is the one keystroke that can.
const maxPasteRunes = 8192

// sanitizePaste makes pasted text safe for the draft while keeping the one
// thing sanitizeKeepingSpace deliberately flattens: the newline.
//
// The typed-text filter turns \n into a space because a lone newline arriving
// in a KeyPressMsg's Text is line-ending noise, not paragraphing. A paste is
// the opposite case — its newlines ARE the operator's structure, the same way
// a vendor's newlines are its paragraphing in sanitize() — and the composer
// has rendered a multi-row draft since ctrl+j existed, so preserving them
// costs nothing the layout has not already budgeted. The draft that renders is
// the string that dispatches, so what the operator pasted is what the vendors
// receive, newlines and all (§7.14's no-silent-divergence rule).
//
// CRLF collapses to one \n rather than to sanitize()'s space-then-newline: the
// Windows clipboard ends every line with \r\n, and splitting the pair would
// gift every pasted line a trailing space the operator never wrote. A bare \r
// is a line ending from an older Mac world and becomes \n for the same reason.
//
// Tabs become a single space — the one lossy rewrite in here, and it is
// stated rather than hidden: a tab has no width a cell grid can budget
// (sanitize()'s rule), so SOME rewrite is forced, and inventing a tab stop
// would be a guess presented as fidelity. A snippet whose indentation is
// load-bearing belongs in a file the brief names. Every other control
// character is dropped, which is what makes the atomicity promise real: a
// pasted \x03 must not cancel, a pasted \x1b must not open an escape sequence,
// and neither has a printable width to claim.
func sanitizePaste(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skipLF := false
	for _, r := range s {
		if skipLF {
			skipLF = false
			if r == '\n' {
				continue
			}
		}
		switch {
		case r == '\r':
			b.WriteByte('\n')
			skipLF = true
		case r == '\n':
			b.WriteByte('\n')
		// Written as escapes on purpose, matching sanitize(): these are
		// invisible in an editor, and this is a place their identity must
		// be exact.
		case r == '\u2028' || r == '\u2029' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// dropped: no width, and possibly a control this room must not obey
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// paste is the one entry point for bracketed-paste text, and the property it
// exists to hold is: a paste NEVER sends. Enter — a keystroke, from a person —
// remains the only way a brief leaves the room.
//
// That property is mostly the runtime's: bubbletea v2.0.8 enables bracketed
// paste unless a View opts out (cursed_renderer.go writes SetModeBracketedPaste;
// council never sets DisableBracketedPasteMode), and ultraviolet's reader
// buffers everything between the paste markers into ONE PasteEvent — a newline
// inside the paste lands as '\n' in Content, never as an Enter keypress, and
// the win32-input-mode encoding Windows Terminal uses is decoded to the same
// place (terminal_reader.go, pinned at ultraviolet v0.0.0-20260703014108).
// What this function adds is the room's half of the contract: the content goes
// into the DRAFT and nowhere else, whatever it carries.
//
// Before this handler existed, Update had no PasteMsg case at all, so a paste
// in any bracketed-paste terminal fell through the type switch and vanished —
// the composer simply never learned the clipboard had been offered. That is
// the defect being fixed: not a paste that fired sends, a paste that did
// nothing.
//
// Insertion is at the caret, which in this composer is always the end of the
// draft — backspace deletes there and typed text lands there, so a paste
// appends for the same reason. navKey reserves left/right for an in-draft
// cursor that does not exist yet; if one arrives, this is one of the places
// that starts caring where it points.
//
// A paste from VIEW mode inserts too, and switches the room to compose. A
// paste is not a keystroke: view mode's letters are commands because they are
// single keys a hand issues one at a time, while pasted text can only ever be
// MATERIAL, and the only place material goes is the draft. Refusing it with
// "press i first" would make the operator perform the same act twice for the
// room's convenience — and the mode line announces compose on the very next
// frame, so the switch is stated, not sprung (§7.8).
//
// The one thing a paste may not do is land while the room is waiting on y or
// n. Not because it would answer — it cannot; the pending flags only read
// keys — but because inserting quietly UNDER a question invites answering
// that question with a draft's worth of stray context on the way, and the
// gate queue's own rule is that nothing about a pending request happens
// implicitly. The paste is refused by name and the question stays exactly
// where it was.
func (m *Model) paste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if msg.Content == "" {
		return m, nil
	}
	if m.st.Gating() {
		m.st.Notice = "a vendor is waiting on you — the paste was not inserted · y approves, n denies"
		return m, nil
	}
	if m.clearPending != "" || m.writePending || m.flowWritePending {
		m.st.Notice = "a question is pending — the paste was not inserted · y or n answers it"
		return m, nil
	}
	offered := utf8.RuneCountInString(msg.Content)
	if have := utf8.RuneCountInString(m.st.Draft); have+offered > maxPasteRunes {
		m.st.Notice = pasteRefusal(offered, have)
		return m, nil
	}
	m.setDraft(m.st.Draft + sanitizePaste(msg.Content))
	if m.st.Mode != ModeComposing {
		m.st.Mode = ModeComposing
		m.st.Help = HelpClosed
	}
	m.st.Notice = ""
	return m, nil
}

// pasteRefusal names the refusal with both numbers — what was offered and what
// the composer holds — because "too big" without the sizes leaves the operator
// unable to tell a near miss from a 2 MB accident, and those call for
// different next moves.
//
// The refusal is ATOMIC: nothing is inserted, rather than a truncated prefix.
// A composer that kept the first 8,192 characters of a paste would be sending
// the vendors a brief the operator never wrote while showing them its tail as
// if it were whole — the silently-rewritten brief this feature refuses to be.
func pasteRefusal(offered, have int) string {
	s := "paste refused: " + itoa(offered) + " chars against the composer's " + itoa(maxPasteRunes)
	if have > 0 {
		s += " (" + itoa(have) + " already in the draft)"
	}
	return s + " — put long text in a file and name the path in the brief"
}
