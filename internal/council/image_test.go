package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/svgframe"
)

// The hero pictures — README.md's, and docs/council.md's since the guide
// stopped quoting a second copy of the same frame as text — are generated from
// this package's own render, and this is the gate that keeps them that way.
//
// They were hand-drawn once, and by PR #125 the council picture was showing a
// room that had not existed for months: a focus mark on every column at once —
// which is §9.12's ambiguous-focus bug, the one the real UI was built to have
// fixed — no seat keys, no focus rail, no posture word in the header, and a
// "Cost: $0.0034" line reading a dollar figure this codebase refuses to derive
// (ADR-001; docs/design.md §4a.1). Every one of those was invisible to CI,
// because a picture nobody generates is a claim with no test under it.
//
// So the picture is the frame. Same State the `hero` golden pins (heroRoom —
// five addressable seats, not room()'s three-column unit fixture), same Render,
// same bytes — only the terminal's chrome is drawn around it. Change how the
// room looks and this fails until the pictures are re-emitted, exactly as a
// golden does.

// heroFrame is the council frame every surface shows: the `hero` golden with
// its all-blank rows dropped. The blank rows are the room's empty scrollback —
// real in a live terminal, dead weight in a picture.
//
// This used to be one of two implementations. The other lived in a test
// asserting the same drop against a copy of the frame quoted as text, first in
// README.md and then in docs/council.md, and the duplication was deliberate:
// if the two transforms ever disagreed, the quoted frame and the picture would
// show different rooms, and one of the two tests would be the one to notice.
// Both quoted copies are gone — a page carrying the same render twice was
// redundancy, and the picture is the copy that carries colour — so there is
// nothing left to disagree with, and this is the only place the drop happens.
func heroFrame(styled string) []string {
	var kept []string
	for _, line := range strings.Split(styled, "\n") {
		if strings.TrimSpace(stripANSI(line)) == "" {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

func TestHeroImagesAreTheRoomThatRenders(t *testing.T) {
	st := heroRoom()
	g := GlyphsFor(false)

	// The coloured render must be the golden's bytes plus escapes and nothing
	// else. This is the join between the two: if styling ever moved a character
	// — a width miscount inside `fit`, a padRight cutting through an escape —
	// the picture would stop being a picture of the golden, and it would be the
	// picture, not the golden, that was wrong.
	plain := Render(st, PlainStyles(), g)
	golden(t, "hero", plain)
	for _, dark := range []bool{true, false} {
		if got := stripANSI(Render(st, NewStyles(dark), g)); got != plain {
			t.Fatalf("the coloured render is not the plain one with escapes added (isDark=%v)\n--- coloured, stripped ---\n%s\n--- plain ---\n%s",
				dark, got, plain)
		}
	}

	// The product seats five addressable vendors. A picture that still says
	// 3/3 is the public claim that drifted after Cursor and Grok landed.
	if !strings.Contains(plain, "5/5 seated") {
		t.Errorf("the hero does not claim five seats are seated:\n%s", plain)
	}
	for _, tag := range []string{"CC", "CX", "AG", "CU", "GR"} {
		if !strings.Contains(plain, tag) {
			t.Errorf("the hero is missing seat tag %q", tag)
		}
	}

	for _, tc := range []struct {
		file    string
		palette svgframe.Palette
		isDark  bool
	}{
		{"telltale-council-dark.svg", svgframe.Dark(), true},
		{"telltale-council-light.svg", svgframe.Light(), false},
	} {
		t.Run(tc.palette.Name, func(t *testing.T) {
			lines := heroFrame(Render(st, NewStyles(tc.isDark), g))
			got, err := svgframe.Render(svgframe.Frame{
				Caption: "telltale council",
				Alt:     "The telltale council room: five vendor seats side by side, the focused seat marked by a rail and a caret, each seat's sandbox posture named in words beneath it.",
				Lines:   lines,
			}, tc.palette)
			if err != nil {
				t.Fatal(err)
			}

			// What the picture SHOWS is the golden, character for character.
			// Byte-equality against the committed file below would pass just as
			// happily on a picture whose text had been mangled into shapes.
			want := make([]string, len(lines))
			for i, l := range lines {
				want[i] = stripANSI(l)
			}
			if rows := svgText(t, got); strings.Join(rows, "\n") != strings.Join(want, "\n") {
				t.Errorf("the picture's text is not the frame's\n--- in the svg ---\n%s\n--- in the frame ---\n%s",
					strings.Join(rows, "\n"), strings.Join(want, "\n"))
			}
			// A dollar figure derived from token counts is on this repo's
			// deliberately-rejected list, and the picture that used to ship one
			// is the reason this line exists.
			if strings.Contains(string(got), "$") {
				t.Error("the picture shows a dollar amount; nothing in the render produces one")
			}
			// §9.12: exactly one seat is addressed, and the caret says which.
			if n := strings.Count(string(got), "▸"); n != 1 {
				t.Errorf("the picture marks %d seats as focused, want exactly 1", n)
			}
			compareImage(t, tc.file, got)
		})
	}
}

// svgText reads the characters an emitted picture actually draws, one row per
// <text> element, unescaped — the reader's side of the picture rather than the
// generator's.
func svgText(t *testing.T, svg []byte) []string {
	t.Helper()
	unesc := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

	var rows []string
	var row strings.Builder
	open := false
	for _, line := range strings.Split(string(svg), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		// The caption strip is chrome, not frame, and it is a one-line <text>
		// with no tspans; skipping it keeps this comparing like with like.
		case strings.HasPrefix(trimmed, "<text") && strings.HasSuffix(trimmed, "</text>"):
		case strings.HasPrefix(trimmed, "<text"):
			open, row = true, strings.Builder{}
		case trimmed == "</text>" && open:
			rows = append(rows, row.String())
			open = false
		case strings.HasPrefix(trimmed, "<tspan") && open:
			inner := trimmed[strings.Index(trimmed, ">")+1:]
			row.WriteString(unesc.Replace(strings.TrimSuffix(inner, "</tspan>")))
		}
	}
	if open {
		t.Fatal("unterminated <text> element in the emitted svg")
	}
	return rows
}

// compareImage is `golden` for the picture files, and it shares the -update
// flag with them: `go test ./internal/council -update` re-emits the goldens and
// the hero pictures together, which is what makes "the picture changed" a thing
// a reviewer reads in a diff instead of a thing nobody notices.
func compareImage(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("..", "..", "images", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/council -update)", err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("images/%s no longer matches the room that renders.\n"+
			"Read what moved, then re-emit it: go test ./internal/council -update", name)
	}
}
