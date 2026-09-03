package council

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheCostIsShownWholeOrNotShown is §4a.1 on the one dollar figure council
// draws. A badge row too narrow to anchor the cost used to trail it behind the
// badges and let the caller's fit() clip it, and fit() clips without an
// ellipsis, so `$0.0041` arrived as `$0.0`: a different, smaller number a
// reader could not tell was cut. The rule now is the one stripBadges already
// applied at fourteen cells: the figure is shown whole, or it is not shown.
//
// Swept over every column width from the strip floor up, over every shape the
// row can take around the figure: the three reference postures, a session
// total (which adds a word the cut used to remove), a replayed room, a shared
// tree with a reason, and a seat with a relayed quota reading. At each width
// the row must carry the whole figure or no `$` at all, and the row must be
// inside w, so the caller's fit() has nothing to clip.
//
// The width check used to compare against the same row WITHOUT a cost, because
// between the strip floor and about twenty-six cells the badge words
// themselves overran and fit() clipped them (`final onl`). That was §9.11's
// own defect, and TestTheBadgeWordsLeaveWholeOrNotAtAll is its record; with
// the words shedding whole at every width the row is asserted inside w
// outright.
func TestTheCostIsShownWholeOrNotShown(t *testing.T) {
	cost := 0.0041
	base := room()
	base.Now = quotaNow

	type shape struct {
		name string
		st   State
		col  Column
		want string // the whole figure, as costCell spells it
	}
	var shapes []shape
	for i, c := range base.Columns {
		c.CostUSD = &cost
		shapes = append(shapes, shape{"seat " + string(rune('1'+i)), base, c, "$0.0041"})
	}
	session := base.Columns[0]
	session.CostUSD, session.CostSession = &cost, true
	shapes = append(shapes, shape{"session total", base, session, "$0.0041 session"})

	replay := base
	replay.Replay = true
	rc := base.Columns[1]
	rc.CostUSD = &cost
	shapes = append(shapes, shape{"replay", replay, rc, "$0.0041"})

	shared := base.Columns[0]
	shared.CostUSD = &cost
	shared.Containment = ContainClaim{Level: ContainShared, Why: "not a git repo"}
	shapes = append(shapes, shape{"shared tree with a reason", base, shared, "$0.0041"})

	quota := base.Columns[0]
	quota.CostUSD = &cost
	quota.Quota = seatQuota(0, quotaWindow("5h", 12, 64*time.Minute))
	shapes = append(shapes, shape{"relayed quota", base, quota, "$0.0041"})

	for _, g := range []Glyphs{UnicodeGlyphs(), GlyphsFor(true)} {
		for _, sh := range shapes {
			shown := 0
			for w := stripWidth; w <= 80; w++ {
				row := badgeRow(sh.st, sh.col, w, PlainStyles(), g)
				if lipgloss.Width(row) > w {
					t.Errorf("%s w=%d: the badge row is %d cells, so fit() would clip it: %q",
						sh.name, w, lipgloss.Width(row), row)
				}
				// fit() is what the column applies to this row (columnChrome),
				// and on a row that fits it may only pad. Stated directly so a
				// change to fit() cannot slip past the width check above.
				if strings.TrimRight(fit(row, w), " ") != strings.TrimRight(row, " ") {
					t.Errorf("%s w=%d: fit() changed the badge row: %q -> %q", sh.name, w, row, fit(row, w))
				}
				switch {
				case strings.Contains(row, sh.want):
					shown++
				case strings.Contains(row, "$"):
					t.Errorf("%s w=%d: a figure that is not %q: %q", sh.name, w, sh.want, row)
				}
			}
			// Not vacuous: at the reference width every shape has room for
			// its figure, so a row that never showed it dropped it for a
			// reason other than width.
			if shown == 0 {
				t.Errorf("%s: the figure was drawn at no width up to 80", sh.name)
			}
		}
	}
}

// TestNarrowColumnsDoNotClipTheCost is the same rule through Render, on the
// room where it was measured: four seats at 120 columns give each 25 cells.
// `WRITES  tokens` leaves room for a figure and the cost is drawn whole;
// `ro:enforced  final only` and `ro:requested  tokens` do not, and their
// seats draw no figure rather than a cut one. Before the fix this frame read
// `$0.0` where a seat had reported $0.0041.
func TestNarrowColumnsDoNotClipTheCost(t *testing.T) {
	fits, tight, tighter := 0.0041, 0.0123, 0.0007
	st := room()
	st.Turn = 1
	st.Columns[0].Sandbox = SandboxClaim{Level: SandboxWrite, Detail: "ungated write posture"}
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = "Tests pass."
	st.Columns[0].CostUSD = &fits
	st.Columns[1].Sandbox = SandboxClaim{Level: SandboxEnforced, Detail: "measured denied shell write"}
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Body = "Done."
	st.Columns[1].CostUSD = &tight
	st.Columns = append(st.Columns, Column{
		Vendor: model.VendorCursor, Label: "Cursor",
		Avail:   AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxRequested, Detail: "ACP plan mode, one trial held"},
		Gran:    GranTokens, Phase: PhaseDone,
		Body:    "Done.",
		CostUSD: &tighter,
	})

	got := render(st)
	if !strings.Contains(got, "$0.0041") {
		t.Error("the seat with room for its figure did not draw it")
	}
	if n := strings.Count(got, "$"); n != 1 {
		t.Errorf("%d dollar figures drawn, want exactly the one that fits whole:\n%s", n, got)
	}
	for _, cut := range []string{"$0.012", "$0.01 ", "$0.000", "$0.00 ", "$0.0 "} {
		if strings.Contains(got, cut) {
			t.Errorf("a figure was clipped to %q:\n%s", cut, got)
		}
	}
	golden(t, "cost-does-not-clip", got)
}
