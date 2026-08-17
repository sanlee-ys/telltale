package council

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
)

// quotaNow is the fixed instant every fixture here is stamped against. A
// literal rather than time.Now(): every age and every countdown in this file is
// arithmetic against State.Now, and a wall clock would make the goldens depend
// on when they were generated.
var quotaNow = time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

// seatQuota builds a reading for a seat: a window label, its percentage, how
// long until it resets, and how old the relayed reading is.
//
// resetIn of zero means the relay carried no reset instant for that window,
// which is a real state — agy's buckets arrive without one when the vendor
// reports neither reset_in_seconds nor reset_time.
func seatQuota(age time.Duration, windows ...model.QuotaWindow) *SeatQuota {
	return &SeatQuota{Windows: windows, WrittenAt: quotaNow.Add(-age)}
}

func quotaWindow(label string, pct float64, resetIn time.Duration) model.QuotaWindow {
	p := model.Percent(pct)
	w := model.QuotaWindow{ID: label, Label: label, UsedPercent: &p}
	if resetIn > 0 {
		t := quotaNow.Add(resetIn)
		w.ResetsAt = &t
	}
	return w
}

// TestASeatWithNoReadingRendersNothingAndAMeasuredZeroRenders is the
// zero-vs-absent rule (§4a.1) on the room's newest number, and it is the first
// thing this feature had to get right.
//
// A vendor at 0% of its window has been MEASURED. A vendor telltale has no
// relayed reading for has not. Collapsing the two would make an unrelayed seat
// look like a fresh account, which is the more dangerous direction of the two:
// it invites a dispatch the room has no evidence will land.
//
// Cursor and grok are in the second class permanently — neither writes quota to
// disk in any form a passive reader can see (§7.17's structurally-absent row) —
// and so is Codex here, whose quota lives in its own store rather than in the
// relay this room reads.
func TestASeatWithNoReadingRendersNothingAndAMeasuredZeroRenders(t *testing.T) {
	st := room()
	st.Now = quotaNow
	// A measured zero: the vendor reported the window and reported nothing used.
	st.Columns[0].Quota = seatQuota(0, quotaWindow("5h", 0, 2*time.Hour))
	// No relay entry at all for the other two.
	got := render(st)
	golden(t, "seat-quota-absent", got)

	if !strings.Contains(got, "5h 0%") {
		t.Errorf("a measured zero did not render:\n%s", got)
	}

	// The absent seats carry no reading and no placeholder for one. Asserted
	// against the row itself rather than against the whole frame, so a `%`
	// somewhere else in the room cannot make this pass.
	for _, i := range []int{1, 2} {
		row := badgeRow(st, st.Columns[i], 38, PlainStyles(), UnicodeGlyphs())
		if strings.Contains(row, "%") {
			t.Errorf("column %d has no relayed reading and rendered one: %q", i, row)
		}
	}

	// And the absent row is byte-identical to the row the same seat drew before
	// this feature existed — an absence costs nothing, not even a space.
	bare := room()
	bare.Now = quotaNow
	for _, i := range []int{1, 2} {
		with := badgeRow(st, st.Columns[i], 38, PlainStyles(), UnicodeGlyphs())
		without := badgeRow(bare, bare.Columns[i], 38, PlainStyles(), UnicodeGlyphs())
		if with != without {
			t.Errorf("column %d: an absent reading changed the badge row\n got %q\nwant %q", i, with, without)
		}
	}
}

// TestTheSeatReadingAndTheRouteAlarmGolden is the present case: a seat whose
// relayed window the vendor itself reports at 100%, a second seat with an
// ordinary reading, and a third with none.
//
// 160 columns, deliberately. At the reference 120 a three-seat grid gives each
// column thirty-eight cells and the badge row already spends most of them on
// the posture claim; the reading is the thing that sheds there, which is the
// designed trade rather than a defect. This golden is the width at which the
// standing figure is on screen, and the shed is asserted separately below.
func TestTheSeatReadingAndTheRouteAlarmGolden(t *testing.T) {
	st := room()
	st.Width = 160
	st.Now = quotaNow
	st.Mode = ModeComposing
	st.Columns[0].Quota = seatQuota(2*time.Hour,
		quotaWindow("5h", 100, 64*time.Minute),
		quotaWindow("7d", 6, 5*24*time.Hour))
	st.Columns[2].Quota = seatQuota(0, quotaWindow("gemini-weekly", 38, 3*time.Hour))
	golden(t, "seat-quota", render(st))
}

// TestSeatQuotaAgeMatchesTheHUDsThresholds. The two constants and the verdict
// word are COPIED from internal/hud rather than imported — the seam this repo
// keeps between the two surfaces (vendorTag's doc comment) — so the copy is
// pinned by literal here, exactly as TestStripTagsMatchTheHUDSpelling pins the
// two-letter tags.
//
// A drift in either direction is a product defect rather than a style one: the
// statusline and the room are usually on screen in the same hour, and one
// calling a reading stale while the other calls it current is the room and the
// gauge disagreeing about the same account.
func TestSeatQuotaAgeMatchesTheHUDsThresholds(t *testing.T) {
	if quotaAgeShown != 5*time.Minute {
		t.Errorf("quotaAgeShown is %v; the HUD carries the age from five minutes", quotaAgeShown)
	}
	if quotaAgeWarn != 5*time.Hour {
		t.Errorf("quotaAgeWarn is %v; §7.17 argues it from Claude's five_hour window", quotaAgeWarn)
	}
	if quotaAgeWord != "stale" {
		t.Errorf("quotaAgeWord is %q; the statusline says %q", quotaAgeWord, "stale")
	}
	if quotaFullPercent != 100 {
		t.Errorf("quotaFullPercent is %v; anything below a hundred is a boundary no vendor published", quotaFullPercent)
	}
}

// TestTheAgeRidesTheReadingAndEscalatesOnTheHUDsThreshold walks the three
// states a relayed reading's age has.
//
// Below quotaAgeShown the statusline is firing often enough that a suffix would
// be noise. From it on the age IS the honesty. Past quotaAgeWarn the reading has
// outlived the fleet's shortest window, and the escalation is carried by the
// WORD first so `--ascii` and NO_COLOR lose nothing.
func TestTheAgeRidesTheReadingAndEscalatesOnTheHUDsThreshold(t *testing.T) {
	g := UnicodeGlyphs()
	for _, tc := range []struct {
		name string
		age  time.Duration
		want string
	}{
		{"fresh", time.Minute, "5h 12%"},
		{"one minute under the age suffix", quotaAgeShown - time.Minute, "5h 12%"},
		{"at the age suffix", quotaAgeShown, "5h 12%  5m ago"},
		{"old but inside the fleet's shortest window", 2 * time.Hour, "5h 12%  2h ago"},
		{"one minute under the warning", quotaAgeWarn - time.Minute, "5h 12%  4h ago"},
		{"at the warning", quotaAgeWarn, "5h 12%  ⚠ stale 5h ago"},
		{"the nineteen-hour reading §7.17 was written for", 19 * time.Hour, "5h 12%  ⚠ stale 19h ago"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := seatQuota(tc.age, quotaWindow("5h", 12, 0))
			forms := quotaForms(q, quotaNow, g)
			if len(forms) == 0 {
				t.Fatal("a seat with a reading produced no form at all")
			}
			// The BAREST rung, which is the one a narrow column reaches: the age
			// has to be on it, because shedding the age re-presents a possibly
			// nineteen-hour-old number as fresh.
			if got := forms[len(forms)-1].plain(); got != tc.want {
				t.Errorf("barest form = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheCountdownShedsAndTheReadingNeverDoes. What sheds is decoration and what
// never sheds is fact — §7.15's cascade at council's scale. The reset countdown
// says when the number will change; the label, the percentage and the age say
// what it is and how much to trust it.
func TestTheCountdownShedsAndTheReadingNeverDoes(t *testing.T) {
	g := UnicodeGlyphs()
	q := seatQuota(2*time.Hour,
		quotaWindow("5h", 12, 64*time.Minute),
		quotaWindow("7d", 6, 5*24*time.Hour))

	forms := quotaForms(q, quotaNow, g)
	if len(forms) != 2 {
		t.Fatalf("want two rungs (dressed, bare), got %d", len(forms))
	}
	if got, want := forms[0].plain(),
		"5h 12% resets 1h04m  7d 6% resets 5d00h  2h ago"; got != want {
		t.Errorf("dressed form = %q, want %q", got, want)
	}
	if got, want := forms[1].plain(), "5h 12%  7d 6%  2h ago"; got != want {
		t.Errorf("bare form = %q, want %q", got, want)
	}
	for _, f := range forms {
		for _, must := range []string{"5h 12%", "7d 6%", "2h ago"} {
			if !strings.Contains(f.plain(), must) {
				t.Errorf("a rung shed %q, which is fact rather than decoration: %q", must, f.plain())
			}
		}
	}

	// A cell too narrow for even the barest rung renders NOTHING rather than a
	// clipped one: a clipped percentage is a different number (stripBadges'
	// ruling). The footer's alarm is what a narrow room keeps instead.
	if _, plain := seatQuotaCell(q, quotaNow, 8, PlainStyles(), g); plain != "" {
		t.Errorf("a reading was clipped into eight cells: %q", plain)
	}
	if _, plain := seatQuotaCell(q, quotaNow, 40, PlainStyles(), g); plain != forms[1].plain() {
		t.Errorf("the widest rung that fits forty cells = %q", plain)
	}
}

// TestTheReadingNeverEvictsAClaimTheBadgeRowAlreadyMade. A new claim takes the
// space that is left, never the space another claim was using: the posture badge
// is what §9.2 refuses to let yield, and the cost is the one figure on this line
// the transcript also records.
func TestTheReadingNeverEvictsAClaimTheBadgeRowAlreadyMade(t *testing.T) {
	cost := 0.0123
	st := room()
	st.Now = quotaNow
	c := st.Columns[0]
	c.CostUSD = &cost
	c.Quota = seatQuota(0, quotaWindow("5h", 12, 64*time.Minute))

	bare := c
	bare.Quota = nil

	for _, w := range []int{24, 28, 32, 38, 48, 60, 80} {
		row := badgeRow(st, c, w, PlainStyles(), UnicodeGlyphs())
		if !strings.Contains(row, "ro:tools") {
			t.Errorf("w=%d: the reading displaced the posture claim: %q", w, row)
		}
		if !strings.Contains(row, "$0.0123") {
			t.Errorf("w=%d: the reading displaced the cost: %q", w, row)
		}
		// The row this seat drew before the reading existed is the ceiling. A
		// narrow badge row already overruns and lets the caller's fit() clip it
		// (badgeRow's own fallback), so the property is not "always inside w" —
		// it is that adding the reading never makes that worse.
		was := badgeRow(st, bare, w, PlainStyles(), UnicodeGlyphs())
		if ceiling := maxInt(w, lipgloss.Width(was)); lipgloss.Width(row) > ceiling {
			t.Errorf("w=%d: the reading widened the badge row from %d to %d cells: %q",
				w, lipgloss.Width(was), lipgloss.Width(row), row)
		}
	}
}

// TestTheRouteCellNamesAFullOrStaleSeat is the footer half (§9.21, amended
// 2026-08-17). The room dispatches from this line, so this is where a reading
// that says the turn may not land has to be readable — at every width, and
// whether or not the seat's own column had room for its figure.
func TestTheRouteCellNamesAFullOrStaleSeat(t *testing.T) {
	full := func() *SeatQuota { return seatQuota(0, quotaWindow("5h", 100, time.Hour)) }
	stale := func() *SeatQuota { return seatQuota(19*time.Hour, quotaWindow("5h", 12, time.Hour)) }
	ok := func() *SeatQuota { return seatQuota(0, quotaWindow("5h", 12, time.Hour)) }

	for _, tc := range []struct {
		name  string
		draft string
		claude,
		agy *SeatQuota
		want string
	}{
		// The vendor's own reading says the window is used up. No verb is added
		// to it: "spent" belongs to §7.17's token line, and the number says the
		// thing on its own.
		{"a full window on the addressed seat", "go", full(), nil, "claude 5h 100%"},
		// Staleness OUTRANKS fullness for one seat, and that is quotaAgeWarn's
		// whole argument: a reading that old may no longer be assumed to
		// describe now, so a stale 100% is not evidence the window is full.
		{"a stale reading outranks a full window", "go",
			seatQuota(19*time.Hour, quotaWindow("5h", 100, time.Hour)), nil,
			"claude stale 19h ago"},
		{"a reading with nothing wrong with it", "go", ok(), nil, ""},
		{"no reading at all", "go", nil, nil, ""},
		// Seated ∩ addressed, the same intersection SeatsIn counts: warning
		// about a seat this turn will not reach is a warning about a turn that
		// does not happen.
		{"the full seat is not addressed", "@agy go", full(), ok(), ""},
		{"the stale seat is the one addressed", "@agy go", ok(), stale(), "agy stale 19h ago"},
		{"an @all turn reaches both", "@all go", ok(), stale(), "agy stale 19h ago"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := room()
			st.Now = quotaNow
			st.Mode = ModeComposing
			st.Draft = tc.draft
			st.Route, _ = ParseRoute(tc.draft)
			st.Columns[0].Quota = tc.claude
			st.Columns[2].Quota = tc.agy

			if got := quotaAlarm(st); got != tc.want {
				t.Fatalf("quotaAlarm = %q, want %q", got, tc.want)
			}
			_, plain := hints(PlainStyles(), UnicodeGlyphs(), modeHints(st, UnicodeGlyphs()))
			cell := UnicodeGlyphs().Warn + " " + tc.want
			if tc.want == "" {
				if strings.Contains(plain, UnicodeGlyphs().Warn) {
					t.Errorf("the footer raised an alarm with nothing to warn about: %q", plain)
				}
				return
			}
			if !strings.Contains(plain, cell) {
				t.Errorf("the footer does not carry %q: %q", cell, plain)
			}
			// It sits against the route it qualifies, in front of everything
			// else on the line.
			if strings.Index(plain, cell) > strings.Index(plain, "enter dispatch") {
				t.Errorf("the alarm sits behind the keys rather than against the route: %q", plain)
			}
		})
	}
}

// TestTheRouteAlarmComputesNothing. §9.21 declined a dollar figure beside the
// seat count because multiplying a count by anything is council deriving a
// number and presenting it as read. The same refusal binds this cell and is
// wider: no total across seats, no count of how many are affected, no
// arithmetic over the percentages at all.
//
// Two seats are full here and exactly one is named — column order decides,
// because ranking a stale reading against a full window would need a
// measurement nobody has, and a count would be the aggregate this cell may not
// compute. What carries the second seat is its own badge row.
func TestTheRouteAlarmComputesNothing(t *testing.T) {
	st := room()
	st.Now = quotaNow
	st.Mode = ModeComposing
	st.Draft = "@all go"
	st.Route, _ = ParseRoute(st.Draft)
	st.Columns[0].Quota = seatQuota(0, quotaWindow("5h", 100, time.Hour))
	st.Columns[2].Quota = seatQuota(0, quotaWindow("gemini-weekly", 100, time.Hour))

	got := quotaAlarm(st)
	if got != "claude 5h 100%" {
		t.Fatalf("quotaAlarm = %q, want the first addressed seat in column order", got)
	}
	if strings.Contains(got, "2") || strings.Contains(got, "seats") {
		t.Errorf("the alarm counted across seats: %q", got)
	}
	if strings.Contains(got, "$") {
		t.Errorf("the alarm quoted money: %q", got)
	}
	// The seat count keeps its present grammar, untouched by any of this.
	if bill := seatBill(st); bill != "(3 seats)" {
		t.Errorf("the seat count changed shape: %q", bill)
	}
}

// TestTheQuotaReadingSurvivesASCII. Every distinction this room makes is carried
// first by a word or a number, with colour and weight only making it easier to
// spot — so the reduced glyph set and NO_COLOR lose nothing but the mark.
func TestTheQuotaReadingSurvivesASCII(t *testing.T) {
	st := room()
	st.Width = 160
	st.Now = quotaNow
	st.ASCII = true
	st.Mode = ModeComposing
	st.Columns[0].Quota = seatQuota(19*time.Hour, quotaWindow("5h", 100, time.Hour))

	got := Render(st, PlainStyles(), GlyphsFor(true))
	for _, want := range []string{
		"5h 100%",                // the reading, digits and a label
		"! stale 19h ago",        // the verdict, in the reduced set's own mark
		"! claude stale 19h ago", // the footer naming the seat
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--ascii dropped %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "⚠") {
		t.Error("the unicode warning mark leaked into an --ascii frame")
	}
}

// TestRenderStaysPureWithAReadingOnScreen. Render reads no clock and no file:
// the age is State.Now minus a stamp, and the stamp arrives on a message. Two
// renders of one State must be identical bytes, or the goldens are flaky in a
// way that only shows up in CI.
func TestRenderStaysPureWithAReadingOnScreen(t *testing.T) {
	st := room()
	st.Now = quotaNow
	st.Mode = ModeComposing
	st.Columns[0].Quota = seatQuota(19*time.Hour, quotaWindow("5h", 100, time.Hour))
	if render(st) != render(st) {
		t.Fatal("Render is not pure over a State carrying a quota reading")
	}
	// And the age is derived from State.Now rather than frozen at read time:
	// a room left open long enough has to watch its reading go stale.
	fresh := st
	fresh.Now = quotaNow.Add(-18 * time.Hour)
	if strings.Contains(render(fresh), quotaAgeWord) {
		t.Error("a reading taken an hour before Now rendered as stale")
	}
}

// TestApplyQuotaClearsAVendorTheReadDropped is the expiry half, and it is the
// one that keeps this honest over a long session.
//
// quotacache self-expires: a window whose reset has passed and any entry over
// 24h old never come back from ReadAll (§7.15), because such a percentage is not
// stale but FALSE. A room that kept its previous reading for a vendor the read
// dropped would be displaying exactly that.
func TestApplyQuotaClearsAVendorTheReadDropped(t *testing.T) {
	m := &Model{st: room(), glyphs: GlyphsFor(false)}
	m.st.Now = quotaNow
	pct := 12.0

	m.applyQuota(quotaMsg{accounts: []quotacache.Account{{
		Vendor:    model.VendorClaude,
		Windows:   []model.QuotaWindow{quotaWindow("5h", pct, time.Hour)},
		WrittenAt: quotaNow,
	}}})
	if m.st.Columns[0].Quota == nil {
		t.Fatal("a relayed reading did not land on its seat")
	}

	// The next read no longer carries claude — the entry aged out.
	m.applyQuota(quotaMsg{})
	if m.st.Columns[0].Quota != nil {
		t.Error("the room kept a reading the relay had already expired")
	}
}

// TestApplyQuotaDropsAWindowWithNoReading. A window relayed for its reset time
// alone says nothing about what is left, which is the only question this line
// exists to answer — and rendered it would be a bare label asserting a
// measurement with nothing measured in it. An account whose every window is like
// that leaves the seat ABSENT rather than present-and-empty.
func TestApplyQuotaDropsAWindowWithNoReading(t *testing.T) {
	m := &Model{st: room(), glyphs: GlyphsFor(false)}
	m.st.Now = quotaNow
	at := quotaNow.Add(time.Hour)

	m.applyQuota(quotaMsg{accounts: []quotacache.Account{{
		Vendor:    model.VendorClaude,
		Windows:   []model.QuotaWindow{{ID: "5h", Label: "5h", ResetsAt: &at}},
		WrittenAt: quotaNow,
	}}})
	if m.st.Columns[0].Quota != nil {
		t.Errorf("a reset time with no reading landed as a reading: %+v", m.st.Columns[0].Quota)
	}
}

// TestAVendorWithNoQuotaAnywhereRendersNothingForever. Cursor and grok are
// §7.17's structurally-absent row: neither writes quota to disk in any form a
// passive reader can see, so no relay entry can ever exist for them and their
// seats carry no reading at any width.
//
// This is asserted rather than assumed because the mechanism is silent — a
// vendor with no cache file simply never appears in ReadAll — and a future
// change that invented a placeholder for "we looked and found nothing" would
// break the zero-vs-absent rule without any test noticing.
func TestAVendorWithNoQuotaAnywhereRendersNothingForever(t *testing.T) {
	st := room()
	st.Now = quotaNow
	st.Columns = append(st.Columns,
		Column{Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxNone}, Gran: GranTokens},
		Column{Vendor: model.VendorGrok, Label: "Grok", Avail: AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxNone}, Gran: GranTokens})

	m := &Model{st: st, glyphs: GlyphsFor(false)}
	// A read that speaks for claude and for nobody else, which is what a machine
	// running the Claude statusline actually produces.
	m.applyQuota(quotaMsg{accounts: []quotacache.Account{{
		Vendor:    model.VendorClaude,
		Windows:   []model.QuotaWindow{quotaWindow("5h", 12, time.Hour)},
		WrittenAt: quotaNow,
	}}})

	for i, c := range m.st.Columns {
		if c.Vendor == model.VendorClaude {
			continue
		}
		if c.Quota != nil {
			t.Errorf("column %d (%s) holds a reading nothing relayed", i, c.Vendor)
		}
		if row := badgeRow(m.st, c, 60, PlainStyles(), UnicodeGlyphs()); strings.Contains(row, "%") {
			t.Errorf("%s rendered a percentage: %q", c.Vendor, row)
		}
	}
	m.st.Mode = ModeComposing
	m.st.Draft = "@cursor @grok go"
	m.st.Route, _ = ParseRoute(m.st.Draft)
	if got := quotaAlarm(m.st); got != "" {
		t.Errorf("the footer warned about a vendor with no quota anywhere: %q", got)
	}
}
