package hud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/svgframe"
)

// README.md's HUD pictures are generated from this package's own render, the
// same way the council pair is (internal/council/image_test.go carries the full
// argument for why the pictures stopped being drawn by hand).
//
// The HUD pair had drifted the same way and worse. The hand-drawn version put a
// context gauge on two Claude rows — 68% and 82% — for the one vendor whose
// adapter documents, field by field, that context_pct is CapNone because the
// window-size denominator is not on its disk (claudecode.go's package doc,
// ADR-001). It also drew a full gauge track beside the em dash on rows that
// have no value at all, which is zero-vs-absent collapsed in a picture. And it
// carried a real developer's home directory into a public repository, against
// the synthesized-fixtures rule.
//
// The frame here sources every one of those from the same golden README.md
// quotes, so an invented percentage would have to be invented in the renderer
// first — where several tests are already waiting for it.

func TestReadmeHeroImagesAreTheHudThatRenders(t *testing.T) {
	var state func() State
	for _, c := range goldenCases() {
		if c.name == "readme" {
			state = c.state
		}
	}
	if state == nil {
		t.Fatal("no `readme` golden case; the README's frame and its picture must come from one fixture")
	}
	g := GlyphsFor(false)

	// Colour must move no character. If it ever does, the picture stops being a
	// picture of the golden README.md quotes, and it is the picture that is
	// wrong.
	plain := Render(state(), PlainStyles(), g)
	for _, dark := range []bool{true, false} {
		if got := stripANSI(Render(state(), NewStyles(dark), g)); got != plain {
			t.Fatalf("the coloured render is not the plain one with escapes added (isDark=%v)\n--- coloured, stripped ---\n%s\n--- plain ---\n%s",
				dark, got, plain)
		}
	}

	for _, tc := range []struct {
		file    string
		palette svgframe.Palette
		isDark  bool
	}{
		{"telltale-hud-dark.svg", svgframe.Dark(), true},
		{"telltale-hud-light.svg", svgframe.Light(), false},
	} {
		t.Run(tc.palette.Name, func(t *testing.T) {
			// Verbatim, blank row and all — unlike council's frame, which drops
			// its empty scrollback. README.md used to quote this golden as text
			// beside the picture; two copies of one render on one screen was
			// redundancy, not belt-and-braces, and the picture is the copy that
			// carries colour. So this is now the only place the frame appears
			// there, and the readback below is what stands in for the text gate
			// the quoted copy used to provide.
			lines := strings.Split(strings.TrimRight(Render(state(), NewStyles(tc.isDark), g), "\n"), "\n")
			got, err := svgframe.Render(svgframe.Frame{
				Caption: "telltale hud",
				Alt:     "The telltale HUD: seven agent sessions from five vendors, each row naming its vendor, workspace, model, context window and age, with the cells no vendor can source left empty rather than filled in.",
				Lines:   lines,
			}, tc.palette)
			if err != nil {
				t.Fatal(err)
			}

			want := make([]string, len(lines))
			for i, l := range lines {
				want[i] = stripANSI(l)
			}
			if rows := svgText(t, got); strings.Join(rows, "\n") != strings.Join(want, "\n") {
				t.Errorf("the picture's text is not the frame's\n--- in the svg ---\n%s\n--- in the frame ---\n%s",
					strings.Join(rows, "\n"), strings.Join(want, "\n"))
			}
			// Cost is a field no adapter in this repo sources, so no picture of
			// this product has a dollar sign in it.
			if strings.Contains(string(got), "$") {
				t.Error("the picture shows a dollar amount; nothing in the render produces one")
			}
			// §4a.1: a computed context percentage is legible AS computed. The
			// marker has to survive into the picture or the picture is making a
			// stronger claim than the HUD did.
			if !strings.Contains(string(got), "~") {
				t.Error("the picture lost the estimate marker on the computed context percentages")
			}
			compareImage(t, tc.file, got)
		})
	}
}

// svgText reads the characters an emitted picture actually draws, one row per
// <text> element, unescaped — the reader's side of the picture rather than the
// generator's. Byte-equality against the committed file would pass just as
// happily on a picture whose text had been mangled into shapes.
func svgText(t *testing.T, svg []byte) []string {
	t.Helper()
	unesc := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

	var rows []string
	var row strings.Builder
	open := false
	for _, line := range strings.Split(string(svg), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		// The caption strip is chrome, not frame: a one-line <text> with no
		// tspans. Skipping it keeps this comparing like with like.
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

// compareImage is compareGolden for the picture files, sharing the same -update
// flag: `go test ./internal/hud -update` re-emits the goldens and the pictures
// together, so "the HUD looks different now" arrives as one reviewable diff.
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
		t.Fatalf("%v (run: go test ./internal/hud -update)", err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("images/%s no longer matches the HUD that renders.\n"+
			"Read what moved, then re-emit it: go test ./internal/hud -update", name)
	}
}
