package hud

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The selection rule, leg by leg (§7.19). Each case is a real fleet shape
// rather than an abstract one, and the ids are the ids the pipeline emits:
// quotacache's convert.go assigns five_hour/seven_day for Claude, the codex
// adapter assigns primary/secondary, and agy's bucket names pass verbatim.
func TestWeeklyWindowsSelectsByVocabularyAndPosition(t *testing.T) {
	w := func(id string) model.QuotaWindow { return model.QuotaWindow{ID: id, Label: id} }
	ids := func(ws []model.QuotaWindow) string {
		var out []string
		for _, x := range ws {
			out = append(out, x.ID)
		}
		return strings.Join(out, ",")
	}

	cases := []struct {
		name string
		in   []model.QuotaWindow
		want string
	}{
		// The last-window leg: the slice's documented order is shortest
		// first, so the last element is the vendor's longest pool.
		{"claude", []model.QuotaWindow{w("five_hour"), w("seven_day")}, "seven_day"},
		{"codex", []model.QuotaWindow{w("primary"), w("secondary")}, "secondary"},
		// The suffix leg plus the dedupe: both vendor-named weekly buckets
		// survive, the five-hour buckets do not, and gemini-weekly is not
		// listed twice for also being last.
		{"agy", []model.QuotaWindow{w("3p-5h"), w("3p-weekly"), w("gemini-5h"), w("gemini-weekly")},
			"3p-weekly,gemini-weekly"},
		// The edge §7.19 documents: a vendor whose only surviving window is
		// short shows that window. It is the longest reading telltale holds,
		// and its own label says how long it is.
		{"expired-seven-day", []model.QuotaWindow{w("five_hour")}, "five_hour"},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := ids(weeklyWindows(c.in)); got != c.want {
			t.Errorf("%s: weeklyWindows = %q, want %q", c.name, got, c.want)
		}
	}
}

// weekModel is a model over the week fixture, sized and pinned the way
// loaded() builds one, with the page not yet open so a test opens it itself.
func weekModel(t *testing.T) *Model {
	t.Helper()
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 18})
	st := weekState(120, 18)
	m.st.Snap = st.Snap
	m.st.Now = pinned
	return m
}

// One body at a time, with the week page in the set: w closes the others,
// the others close w, and esc unwinds the week page first.
func TestWeekPageIsOneBodyAmongEqual(t *testing.T) {
	m := weekModel(t)

	m = send(t, m, key("w"))
	if !m.st.Week {
		t.Fatal("w did not open the week page")
	}
	m = send(t, m, key("u"))
	if m.st.Week || !m.st.Usage {
		t.Fatalf("u over the week page: Week=%v Usage=%v, want the usage view alone", m.st.Week, m.st.Usage)
	}
	m = send(t, m, key("w"))
	if m.st.Usage || !m.st.Week {
		t.Fatalf("w over the usage view: Week=%v Usage=%v, want the week page alone", m.st.Week, m.st.Usage)
	}
	m = send(t, m, key("esc"))
	if m.st.Week {
		t.Fatal("esc did not close the week page")
	}
}

// The spend ban, asserted over the whole frame the way the cursor retirement
// is: the fixture carries agy token counts the u page renders, and no width
// and neither glyph set may put them on the week page (§7.19).
func TestTheWeekPageRendersNoSpend(t *testing.T) {
	for _, width := range []int{120, 80, 60} {
		for _, ascii := range []bool{false, true} {
			st := weekState(width, 24)
			frame := Render(st, PlainStyles(), GlyphsFor(ascii))
			for _, marker := range []string{"uncached in", "spent", "on disk"} {
				if strings.Contains(frame, marker) {
					t.Errorf("width %d ascii %v: the week page renders spend (%q):\n%s",
						width, ascii, marker, frame)
				}
			}
		}
	}
}

// The five-hour buckets in the fixture must not reach the page. The absence
// claim is the page's whole point, so it is pinned directly rather than left
// to the golden's eyeball.
func TestTheWeekPageDropsTheShortWindows(t *testing.T) {
	frame := Render(weekState(120, 24), PlainStyles(), UnicodeGlyphs())
	for _, short := range []string{"3p-5h", "gemini-5h"} {
		if strings.Contains(frame, short) {
			t.Errorf("a short window (%q) reached the week page:\n%s", short, frame)
		}
	}
	for _, want := range []string{"3p-weekly", "gemini-weekly", "7d"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the week page is missing %q:\n%s", want, frame)
		}
	}
}

// §7.15 through the lens: a relayed reading past quotaAgeWarn carries the
// word, the glyph and the age on its own row, and a fresh relayed reading in
// the same frame carries none of it.
func TestTheWeekPageEscalatesTheStaleRelay(t *testing.T) {
	frame := Render(weekStaleState(120, 18), PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(frame, quotaAgeWord+" 19h ago") {
		t.Fatalf("the stale relay row does not carry %q:\n%s", quotaAgeWord+" 19h ago", frame)
	}
	if strings.Count(frame, quotaAgeWord) != 1 {
		t.Fatalf("the escalation leaked past the one stale vendor:\n%s", frame)
	}
}
