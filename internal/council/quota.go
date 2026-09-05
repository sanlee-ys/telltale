package council

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// The seat quota reading (design.md §9.21, amended 2026-08-17).
//
// The room dispatches a turn to between one and five vendor accounts and, until
// now, said nothing at all about how much of any of them is left. telltale
// already holds the answer: the statusline relays what it just rendered to
// ~/.telltale/quota/<vendor>.json (§7.15), and the HUD has read it since
// 2026-08-07. Council shipped that relay and did not read it, so the one surface
// that SPENDS the quota was the one surface blind to it.
//
// **This is a read and only a read.** Council writes nothing here — not the
// relay, not a cache of its own. The read/write boundary (CLAUDE.md) already
// grants council exactly one write, `council/room.json`, and a second one would
// have to be argued from scratch. What follows is therefore as old as the last
// statusline render in that vendor, which is why the age travels with the
// reading everywhere it is drawn.
//
// **§7.17's declined "per-row quota" does not bind here, and the reason is
// arithmetic rather than taste.** That ruling refuses a quota cell on a HUD
// ROW, because a row is one session and an account fact repeated per session
// asserts a per-session limit that does not exist — five Claude sessions would
// draw the same 42% five times and read as five separate budgets. A council
// SEAT is not a row. There is exactly one seat per vendor in a room (seats.go),
// so a seat reading is an account reading printed once against the account it
// describes. The distinction §7.1's sixth rule protects is intact: nothing here
// is ever drawn per session, per turn or per column of the same vendor.
//
// **What renders, and what never does:**
//
//   - a TEXT reading, never a gauge. The HUD spends a track on this because it
//     has a header line to spend; council would need a new fill colour to draw
//     one, which re-opens both the `isDark` ruling and style.go's "council adds
//     no hues of its own". The window's own label and its own percentage carry
//     the whole fact in eight cells, and they survive --ascii and NO_COLOR
//     unchanged because they are words and digits.
//   - only a window the vendor put a READING on. A window relayed for its reset
//     time alone says nothing about what is left, which is the only question
//     this line exists to answer.
//   - a measured zero as `5h 0%`, and no reading at all as nothing. That is
//     §4a.1's zero-vs-absent rule, and `seat-quota-absent.txt` is the golden
//     that pins it. Cursor and grok have no quota anywhere on disk (§7.17's
//     structurally-absent row), so their seats render nothing here forever —
//     and nothing is what an unrelayed Claude seat renders too, because both
//     are the same fact: this room has no reading.
//   - never a total across seats, never a dollar figure, never a percentage
//     multiplied by anything. Every number on screen is one vendor's own,
//     printed against that vendor's own window label.
type SeatQuota struct {
	// Windows are the windows quotacache returned for this vendor, verbatim
	// and in the order it returned them, filtered to the ones carrying a
	// reading. quotacache has already dropped a window whose reset has passed
	// (its percentage is not stale, it is false) and any entry over 24h old.
	Windows []model.QuotaWindow

	// WrittenAt is when the statusline took the reading, not when council read
	// the file. The age the room displays is measured from this, against
	// State.Now — so the age is a fact about the MEASUREMENT and advances on
	// the tick like every other clock in this room.
	WrittenAt time.Time
}

// quotaAgeShown and quotaAgeWarn are the HUD's two age thresholds, COPIED
// rather than imported — the same seam vendorTag copies across, for the same
// reason. internal/council and internal/hud share the normalized session model
// and internal/theme's numbers and nothing else, and reaching into the HUD for
// a rendering constant is the coupling that seam exists to prevent.
//
// TestSeatQuotaAgeMatchesTheHUDsThresholds pins both by literal so the copy
// cannot drift in silence. What each one means is argued once, in
// internal/hud/usage.go, and deliberately not re-argued here:
//
//   - quotaAgeShown (5m): below it the statusline is firing often enough that
//     an age would be noise; from it on, the age IS the honesty.
//   - quotaAgeWarn (5h): the shortest quota window telltale has measured
//     anywhere in the fleet (Claude's five_hour, §3.1). A reading older than
//     that has outlived the fastest-moving quota this product knows about, so
//     the reader may no longer assume the number describes now.
const (
	quotaAgeShown = 5 * time.Minute
	quotaAgeWarn  = 5 * time.Hour
)

// quotaAgeWord is the HUD's verdict word for an over-age relayed reading, and
// it is copied for the stronger version of the reason above: this is
// VOCABULARY, not a threshold. A reader who learned `⚠ stale 19h ago` on the
// statusline must meet the same three words in the room, in the same order,
// with the same age beside them. A second spelling of one state is the failure
// vendorTag's doc comment describes — "one product, one vocabulary" — and it is
// worse here, because the two surfaces are usually on screen in the same hour.
const quotaAgeWord = "stale"

// quotaFullPercent is where a window stops having room in it.
//
// A hundred, and it is the one threshold in this file council did not have to
// invent: it is the vendor's own statement that the window is used up. Ninety,
// or "nearly full", would be council picking a severity boundary no vendor
// published — the same class of guess as filling a CapNone field with a
// plausible value (§4a.1). Below a hundred the seat still prints its
// percentage, and the reader draws their own line.
const quotaFullPercent = 100

// The word "spent" is deliberately NOT used for a full window, anywhere on this
// surface. §7.17 owns it for TOKEN counts — `spent  uncached in 1.2M · out
// 13.1k` — and quota and spend are the two claims that view exists to keep
// apart ("a reading against a limit" against "a count with no denominator").
// Lending quota the spend line's verb would blur them in the one room where
// both vendors' numbers are on screen at once. What the room says instead is
// the vendor's own reading, `5h 100%`, which needs no verb at all.

// quotaMsg is one finished read of the relay arriving back in the Update loop.
//
// It carries the whole fleet rather than one vendor, because the read is one
// directory listing: splitting it per vendor would spend N messages on one
// syscall and let the room hold a half-applied fleet between them.
type quotaMsg struct {
	accounts []quotacache.Account
}

// readQuotaCmd reads the relay off the Update loop.
//
// A Cmd rather than an inline read, and this is the rule rather than a
// preference: Render must stay pure over State (TestRenderIsPure), and the read
// touches the filesystem. It is also not on the tick — the file only changes
// when the user's statusline fires, so polling it ten times a second would
// spend a directory listing per frame to re-read bytes that did not move.
//
// It runs at room open and after each turn, which are the two moments the
// answer can have changed in a way that matters: the room was just opened
// against whatever the statusline last wrote, and a turn just consumed some of
// it. The second one lands late by construction — council does not write the
// relay, so a turn's cost appears here only after that vendor's statusline
// renders again — and that is exactly what the age suffix is for.
//
// time.Now() is read HERE, in the goroutine, and never in Render. quotacache's
// reader needs a clock to expire entries against, and State.Now is stamped on
// the tick, which is a moment already past by the time this runs.
func readQuotaCmd() tea.Cmd {
	return func() tea.Msg {
		dir, err := quotacache.Dir()
		if err != nil {
			// No home directory means no relay to read. An empty result CLEARS
			// what the room is holding, which is the honest answer: the room
			// cannot see the readings any more, so it stops showing them
			// (§7.7 shows less on failure, never an error banner).
			return quotaMsg{}
		}
		return quotaMsg{accounts: quotacache.ReadAll(dir, time.Now())}
	}
}

// applyQuota lands one read on the columns, and CLEARS every seat the read did
// not speak for.
//
// The clear is the load-bearing half. quotacache self-expires: a window whose
// reset has passed and an entry over 24h old never come back from ReadAll
// (§7.15). A room that kept its previous reading for a vendor the read dropped
// would be displaying a percentage §7.15 calls not stale but FALSE, which is
// the one thing the expiry rule exists to prevent.
func (m *Model) applyQuota(msg quotaMsg) {
	byVendor := make(map[model.VendorID]*SeatQuota, len(msg.accounts))
	for _, a := range msg.accounts {
		// Only windows carrying a reading. A window relayed for its reset time
		// alone would render as a bare label — a row asserting it measured
		// something, with nothing measured in it.
		var windows []model.QuotaWindow
		for _, w := range a.Windows {
			if w.UsedPercent != nil {
				windows = append(windows, w)
			}
		}
		if len(windows) == 0 || a.WrittenAt.IsZero() {
			continue
		}
		byVendor[a.Vendor] = &SeatQuota{Windows: windows, WrittenAt: a.WrittenAt}
	}
	for i := range m.st.Columns {
		m.st.Columns[i].Quota = byVendor[m.st.Columns[i].Vendor]
	}
}

// quotaForm is one rung of the seat reading's shed ladder, split into the two
// parts that are styled differently.
//
// Split rather than kept as one string because of §9.5's ANSI trap: the width
// arithmetic in badgeRow runs over the plain text, and a form assembled with
// escapes already in it is a string the narrow-width paths would cut through.
type quotaForm struct {
	// reading is the windows: `5h 12%  7d 6%`, or with countdowns at the top
	// rung. Muted — it is a standing fact about the account, not news.
	reading string
	// age is the measurement's own time: `2h ago`, or `⚠ stale 19h ago` past
	// quotaAgeWarn. Empty below quotaAgeShown.
	age string
	// stale reports that age carries the escalated form, so the renderer knows
	// to spend severity on it rather than mute it.
	stale bool
}

func (f quotaForm) plain() string {
	if f.age == "" {
		return f.reading
	}
	return f.reading + "  " + f.age
}

func (f quotaForm) render(sty Styles) string {
	// The reading is the vendor's own relayed number, so it takes the MEASURED
	// ink rather than chrome (MONOGRAPH, style.go). The AGE beside it is a
	// statement about the measurement rather than a measurement, which is the
	// separation §7.17 already draws one surface over — so it stays chrome until
	// it goes stale and becomes a severity.
	out := sty.Measured.Render(f.reading)
	if f.age == "" {
		return out
	}
	ageStyle := sty.Muted
	if f.stale {
		// The whole suffix at severity, not the four characters of the age:
		// the verdict is what changed, and a string with escapes in its middle
		// is one the narrow-width paths may cut through (CLAUDE.md's ANSI
		// trap). The reading itself is untouched — its percentage is a
		// statement about the ACCOUNT and this is a statement about the
		// MEASUREMENT, which is the separation §7.17 draws one surface over.
		ageStyle = sty.SevWarn
	}
	return out + "  " + ageStyle.Render(f.age)
}

// quotaForms is the seat reading's shed ladder, most dressed first — the same
// idiom stripHeader and the overflow marker use, so a reader of this package
// meets one shedding shape rather than three.
//
// What sheds is decoration and what never sheds is fact, which is §7.15's own
// cascade at council's scale:
//
//   - the RESET COUNTDOWN sheds. It says when the number will change; the
//     number says what it is now, and a column is thirty-six cells.
//   - the WINDOW LABEL, the PERCENTAGE and the AGE never shed. Dropping a
//     label would leave two bare percentages of unnamed windows; dropping the
//     age would re-present a possibly-nineteen-hour-old reading as fresh,
//     which is the defect quotaAgeWarn was added for in the first place.
//
// There is no rung below the barest one, and the whole cell is dropped instead
// of clipped — stripBadges' ruling, that a clipped state word is not a word.
// What makes a silent drop affordable here is that the footer's own cell
// (quotaAlarm) keeps naming a seat whose reading is full or stale at every
// width, so a narrow room loses the standing figure, never the alarm.
func quotaForms(q *SeatQuota, now time.Time, g Glyphs) []quotaForm {
	if q == nil || len(q.Windows) == 0 {
		return nil
	}
	age, stale := "", false
	if d := now.Sub(q.WrittenAt); d >= quotaAgeWarn {
		// The word first, the glyph second, the hue third — §7.1 rule 2's
		// order, and the reason the reduced glyph set costs this nothing: `!`
		// is a weaker mark than `⚠`, so the sentence has to stand without
		// either. Same three words, same order, same age the statusline says.
		age, stale = g.Warn+" "+quotaAgeWord+" "+theme.Age(d)+" ago", true
	} else if d >= quotaAgeShown {
		age = theme.Age(d) + " ago"
	}

	dressed := make([]string, 0, len(q.Windows))
	bare := make([]string, 0, len(q.Windows))
	for _, w := range q.Windows {
		// Guarded by applyQuota, and re-checked because quotaForms is reachable
		// from a State a test typed out by hand.
		if w.UsedPercent == nil {
			continue
		}
		cell := w.Label + " " + theme.Percent(float64(*w.UsedPercent))
		bare = append(bare, cell)
		if w.ResetsAt != nil {
			if d := w.ResetsAt.Sub(now); d > 0 {
				// The WORD "resets", not a glyph. The HUD spends `↻` on this
				// because its header has a legend beside it in every dress
				// level; council's Glyphs has no slot for it, and minting one
				// would be a new mark in a set §9.26 keeps deliberately small.
				// The word costs five cells and needs no legend at all.
				cell += " resets " + theme.Countdown(d)
			}
		}
		dressed = append(dressed, cell)
	}
	if len(bare) == 0 {
		return nil
	}
	forms := []quotaForm{{reading: strings.Join(bare, "  "), age: age, stale: stale}}
	if d := strings.Join(dressed, "  "); d != forms[0].reading {
		forms = append([]quotaForm{{reading: d, age: age, stale: stale}}, forms...)
	}
	return forms
}

// seatQuotaCell picks the widest rung that fits `avail` cells, or nothing.
//
// It returns the plain text as well as the styled string, because badgeRow's
// gap arithmetic is over the plain copy — see quotaForm.
func seatQuotaCell(q *SeatQuota, now time.Time, avail int, sty Styles, g Glyphs) (styled, plain string) {
	for _, f := range quotaForms(q, now, g) {
		// lipgloss.Width, never len: `⚠` is three bytes and one cell, and this
		// package measures cells everywhere for exactly that reason.
		if p := f.plain(); lipgloss.Width(p) <= avail {
			return f.render(sty), p
		}
	}
	return "", ""
}

// quotaAlarm names the first addressed seat whose relayed reading says this
// turn may not land the way the reader expects.
//
// **It names, and it computes nothing.** §9.21's cell already refuses a dollar
// figure beside the seat count on the argument that multiplying a count by
// anything is council deriving a number and presenting it as read. The same
// refusal binds here and is wider: no total across seats, no average, no
// percentage arithmetic of any kind. What this cell prints is one vendor's id
// and one of that vendor's own window readings, copied.
//
// **Seated ∩ addressed**, the same intersection State.SeatsIn counts and
// dispatch loops over. A route may name a vendor that is not installed or that
// --vendor left out of the room; that seat is never spawned, so warning about
// its account would be a warning about a turn that does not happen.
//
// **Staleness outranks fullness for one seat**, and that ordering is the whole
// argument for quotaAgeWarn: a reading past it may no longer be assumed to
// describe now, so a stale 100% is not evidence the window is full — it is
// evidence the room does not know. Reporting it as `100%` would be the
// nineteen-hour incident (§7.17) reproduced in a new room.
//
// **Exactly one seat is named, and a second is not counted.** Column order
// decides, because ranking two seats would mean ranking a stale reading against
// a full window, and there is no measurement behind such an order. A count —
// `2 seats` — is the aggregate across seats this cell may not compute. What
// carries the rest is each seat's own badge row, where the reading sits against
// the seat it belongs to. Recorded as a limitation in §9.21's amendment rather
// than left to be discovered.
func quotaAlarm(st State) string {
	for _, c := range st.Columns {
		if !st.seats(c) || !st.Route.addresses(c.Vendor) || c.Quota == nil {
			continue
		}
		if d := st.Now.Sub(c.Quota.WrittenAt); d >= quotaAgeWarn {
			return string(c.Vendor) + " " + quotaAgeWord + " " + theme.Age(d) + " ago"
		}
		for _, w := range c.Quota.Windows {
			if w.UsedPercent != nil && float64(*w.UsedPercent) >= quotaFullPercent {
				// The window's own label and its own number, so what the footer
				// says is exactly what that seat's badge row says. One fact,
				// one spelling.
				return string(c.Vendor) + " " + w.Label + " " +
					theme.Percent(float64(*w.UsedPercent))
			}
		}
	}
	return ""
}

// The gauges as ROUTING (design.md §9.56).
//
// Once seats take briefs one at a time (§9.54) the question the readings
// exist to answer changes shape. On a committee the reading said "this turn
// may not land"; on a crew it says "which seat has headroom for THIS brief" —
// and that is a question about the route, asked while the route can still be
// changed, so it is answered in the routing cell and nowhere else.
//
// Everything below is a READ of the same relayed readings quotaAlarm reads,
// under the same rules, and it adds no number the relay did not carry:
//
//   - the hint names one seat, one window and that window's own percentage,
//     copied. No total, no average, no arithmetic over two seats' figures.
//   - `@auto` RANKS, and ranking is the one operation here that is not a copy.
//     What it ranks is headroom — a hundred minus the vendor's own used
//     percentage, in the vendor's own shortest window — over seats that HAVE
//     a reading. A seat with none is never ranked, because a rank needs a
//     number and the honest number for that seat is absence (§4a.1). Cursor
//     and grok are that seat forever (§7.17), and so is an unrelayed Claude.
//   - a reading that no longer describes now is absent, not a number. Two
//     cases: a window whose reset has passed (quotacache drops it on read,
//     and the room's own clock can pass it between reads), and a reading past
//     quotaAgeWarn (quotaAlarm names that one as stale; this file ranks and
//     warns on it as if it were not there).
//   - the hint STOPS at a hundred. A full window is quotaAlarm's cell, with
//     the warning mark, and printing it twice on one line would be one fact
//     in two cells.

// HeadroomWarnDefault is where the routing cell starts naming a window: at
// or above this percent used. It is the one threshold in this file council
// DID pick, and --headroom-warn exists precisely because it is a pick rather
// than a vendor's statement: the number is the operator's to move, and the
// cell prints the vendor's own percentage beside it so the reader can see the
// figure the threshold was applied to.
const HeadroomWarnDefault = 90

// autoRefusal is what enter says when `@auto` has nothing to choose from.
const autoRefusal = "@auto needs a measured reading; none of the seated seats has one"

// headroomThreshold is the room's threshold, with the flag's default standing
// in for a State that never set one (every State a test types out by hand).
func headroomThreshold(st State) int {
	if st.HeadroomWarn <= 0 {
		return HeadroomWarnDefault
	}
	return st.HeadroomWarn
}

// measuredWindows is the windows of one reading that still describe `now`:
// each carries a percentage, none has reset, and the reading as a whole is
// not past quotaAgeWarn. Nil for a seat with no reading, and nil for a seat
// whose reading has gone stale — the same answer on purpose, because both
// are a seat this file may not put a number on.
func measuredWindows(q *SeatQuota, now time.Time) []model.QuotaWindow {
	if q == nil || now.Sub(q.WrittenAt) >= quotaAgeWarn {
		return nil
	}
	var out []model.QuotaWindow
	for _, w := range q.Windows {
		if w.UsedPercent == nil {
			continue
		}
		if w.ResetsAt != nil && !w.ResetsAt.After(now) {
			// The window this percentage described no longer exists; the
			// number is not stale, it is false (§7.15).
			continue
		}
		out = append(out, w)
	}
	return out
}

// shortestWindow is the window that resets soonest, which is the one whose
// headroom binds the next brief. A window with no reset instant sorts behind
// every window that has one — agy's buckets arrive that way — and among
// those, or when none carries a reset, the vendor's own order decides.
func shortestWindow(ws []model.QuotaWindow) (model.QuotaWindow, bool) {
	if len(ws) == 0 {
		return model.QuotaWindow{}, false
	}
	best := ws[0]
	for _, w := range ws[1:] {
		switch {
		case w.ResetsAt == nil:
			continue
		case best.ResetsAt == nil, w.ResetsAt.Before(*best.ResetsAt):
			best = w
		}
	}
	return best, true
}

// usedCell is the window's label and its own percentage, with the word the
// routing cell spends: `5h 94% used`. The badge row prints the same label and
// the same figure without the word; the word is here because this cell sits
// beside a seat NAME, where a bare `5h 94%` reads as a duration and a score.
func usedCell(w model.QuotaWindow) string {
	return w.Label + " " + theme.Percent(float64(*w.UsedPercent)) + " used"
}

// headroomWarning names the first addressed, seated seat with a measured
// window at or above the threshold and under a hundred, and that window's
// reading. Empty when no addressed seat is there.
//
// Seated ∩ addressed, quotaAlarm's intersection: a warning about a seat this
// brief will not reach is a warning about a turn that does not happen. Column
// order decides between two, on quotaAlarm's argument — ranking two warnings
// would need a measurement nobody has, and each seat's own badge row carries
// the rest.
func headroomWarning(st State) (model.VendorID, string) {
	limit := float64(headroomThreshold(st))
	for _, c := range st.Columns {
		if !st.seats(c) || !st.Route.addresses(c.Vendor) {
			continue
		}
		for _, w := range measuredWindows(c.Quota, st.Now) {
			if pct := float64(*w.UsedPercent); pct >= limit && pct < quotaFullPercent {
				return c.Vendor, usedCell(w)
			}
		}
	}
	return "", ""
}

// autoPick is `@auto`'s choice: among seated, IDLE seats with a measured
// reading, the one with the most headroom in its shortest window. Ties go to
// column order. ok is false when no seated seat has a reading, which is the
// refusal and never a fallback to the default route — a brief the operator
// handed to the readings must not go quietly to Claude because the readings
// were empty.
//
// Idle is the column's own phase (Column.inFlight), so Render can ask the same
// question the dispatch asks: a seat mid-answer cannot take this brief
// (§9.54), and picking it would resolve `@auto` to a refusal.
func autoPick(st State) (model.VendorID, string, bool) {
	var (
		pick   model.VendorID
		cell   string
		best   float64
		picked bool
	)
	for _, c := range st.Columns {
		if !st.seats(c) || c.inFlight() {
			continue
		}
		w, ok := shortestWindow(measuredWindows(c.Quota, st.Now))
		if !ok {
			continue
		}
		room := quotaFullPercent - float64(*w.UsedPercent)
		if !picked || room > best {
			pick, cell, best, picked = c.Vendor, usedCell(w), room, true
		}
	}
	return pick, cell, picked
}

// routeCell is the routing cell's key text: the route, qualified by what the
// readings say about it.
//
//	→ codex                       nothing to add
//	→ codex · 5h 94% used         the addressed seat's window is near its limit
//	→ everyone · codex 5h 94% used  the same, on a route that names a set
//	→ auto: grok (5h 12% used)    what @auto resolved to on this frame
//	→ auto: no measured reading   what enter will refuse, said first
//
// The seat name is dropped when the route already IS that seat, and kept
// otherwise, so the cell never says `codex` twice and never leaves a reader
// guessing which of `everyone` it means.
func routeCell(st State) string {
	if st.Route.Auto && !st.Route.Mixed {
		if v, w, ok := autoPick(st); ok {
			return "→ auto: " + string(v) + " (" + w + ")"
		}
		return "→ auto: no measured reading"
	}
	label := routeLabel(st)
	v, w := headroomWarning(st)
	switch {
	case w == "":
		return "→ " + label
	case label == string(v):
		return "→ " + label + " · " + w
	default:
		return "→ " + label + " · " + string(v) + " " + w
	}
}

// dispatchAuto is enter on an `@auto` draft. It reports whether it handled the
// keystroke; false means the draft is not an auto route and the ordinary
// dispatch runs.
//
// The pick is made HERE, at enter, against the same State the footer just
// rendered its cell from — so what the cell said is what the room does. The
// draft is rewritten to name the seat (`@auto fix it` becomes `@grok fix
// it`) and handed to dispatch unchanged from there: the header, the
// transcript and room.json then record a turn to grok, which is the truth,
// and `@auto` leaves no second route shape for those surfaces to learn. The
// notice says the pick and its reading so the choice is on screen after the
// cell that made it has been cleared.
//
// A refused dispatch (every seat busy, nothing seated) puts the operator's own
// words back: the rewrite was the room's, and a draft the room edited and
// then failed to send would leave them editing text they did not type.
func (m *Model) dispatchAuto() (tea.Cmd, bool) {
	r := m.st.Route
	if !r.Auto {
		return nil, false
	}
	if r.Mixed {
		m.st.Notice = "@auto picks the seat itself — drop the other mentions"
		return nil, true
	}
	v, w, ok := autoPick(m.st)
	if !ok {
		m.st.Notice = autoRefusal
		return nil, true
	}
	typed := m.st.Draft
	_, brief := ParseRoute(typed)
	m.setDraft("@" + string(v) + " " + brief)
	cmd := m.dispatch()
	if m.turnOf(v) == nil && !ackWants(m.st) {
		// Refused: dispatch wrote the reason. The draft it left is the
		// rewritten one, so the typed one goes back.
		//
		// A HELD card is not a refusal, which is why it is asked about here
		// (ack.go). The seat is picked, the brief is the room's to send, and
		// the operator is one keystroke from sending it; putting `@auto` back
		// in the composer would offer them a second pick they never asked for,
		// and the notice below is the one that says which seat the card is
		// about.
		m.setDraft(typed)
		return cmd, true
	}
	m.st.Notice = "@auto → " + string(v) + " (" + w + ")"
	return cmd, true
}
