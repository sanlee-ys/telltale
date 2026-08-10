package hud

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// The fleet usage view (design.md §7.17).
//
// The header answers "am I about to run out?" in a glance and has a hard
// budget of one or two lines to do it in. This surface answers the slower
// question underneath it — what can telltale actually say about each vendor's
// account, and where a vendor is blank, WHY — and it is the home §7.10 asked
// for when it recorded that "a second quota-bearing vendor needs a per-vendor
// block".
//
// One organizing rule shapes everything below, and it came out of the
// measurement rather than out of a layout preference: "usage" is TWO different
// claims and this view may never blur them.
//
//   - QUOTA is a reading against a limit the vendor published. It gets the
//     gauge, the percentage and the reset countdown, because all three are
//     meaningful only where a denominator exists.
//   - SPEND is a count of tokens with no denominator anywhere (§7.16). It gets
//     none of that vocabulary — no bar, no percentage, no countdown — because
//     each of them would invent a ceiling, which is the same class of error as
//     filling a CapNone field with a plausible guess. It gets a verb and its
//     accumulation window instead.
//
// The spend half was reshaped on 2026-08-09 by the owner's ruling (§7.16 and
// §7.17 amendments). Cursor's relayed total no longer renders anywhere; agy's
// does, and it is a DIFFERENT kind of sum, which is why the window wording
// differs too. Cursor's was a meter — a file that only ever went up. agy's is
// a scan, summed over the conversations that are on disk at this moment, so
// deleting a conversation makes it smaller. A window that said "since <date>"
// would be a claim about a meter this number is not, and the wording the spend
// line actually uses says "on disk" at every dress level for exactly that
// reason.
//
// It REPLACES the row area rather than floating over it, the detail pane's
// precedent (§7.11): a panel covering the thing being monitored is a monitor
// you have to move to read.
//
// It is also deliberately NOT filtered by `v` or by the find query. Those
// narrow the SESSION list, and nothing on this surface is a session fact —
// filtering an account reading by a session filter would be the per-row quota
// §7.1 forbids, arriving by the back door.

const (
	// usageIndent hangs the facts off the same column the detail pane uses, so
	// the two bodies read as one product rather than two panels.
	usageIndent = 8
	// usageLabel is the label column: a quota window's label, or the spend
	// line's verb. 13 columns because "gemini-weekly" is exactly 13 and a
	// window label truncated to "gemini-week…" would stop naming the window
	// the number belongs to.
	usageLabel = 13
	usageGap   = 2

	// usagePct is one column wider than the 5 that "99.9%" needs, matching the
	// detail pane: this surface has room for the estimate marker without
	// stealing it from anything.
	usagePct = 6

	// usageGaugeWide is the whole point of giving quota its own surface. The
	// header's bar is 8 cells because it is sharing one line with every other
	// vendor; here there is one window per line, so the bar gets room to be
	// read rather than merely glanced at. Fill resolution at 20 cells is one
	// eighth of 1/19th — 0.66% — against the header's 1.6%.
	usageGaugeWide = 20
	// usageGaugeCompact and the narrow tier's zero follow the grid's own shed
	// order (§7.2): the bar goes before the number, because the bar is a
	// redundant encoding of a number that stays on screen.
	usageGaugeCompact = 12
)

// usageGaugeFor sizes the bar for a terminal width, on the grid's breakpoints
// rather than on new ones. A second set of breakpoints is a second thing to
// keep in step with §7.2.
func usageGaugeFor(width int) int {
	switch tierFor(width) {
	case TierWide:
		return usageGaugeWide
	case TierCompact:
		return usageGaugeCompact
	default:
		return 0
	}
}

// usageBlock is one vendor's standing: what it can say about its quota, what it
// can say about its spend, and how many sessions it has on this machine.
//
// The session count is not displayed. It decides whether the vendor appears at
// all: a vendor with no quota, no spend and no sessions is not on this machine
// in any sense telltale measured, and listing it would make the view a
// checklist of every adapter that was compiled in rather than a report on what
// is actually running.
type usageBlock struct {
	vendor   model.VendorID
	quota    *quotaVendorBlock
	spend    *scanSpend
	sessions int
}

// scanSpend is one vendor's token consumption as this scan measured it: the
// sum of the per-session counts, and the number of sessions that contributed
// one.
//
// The session count is not decoration — it IS the accumulation window, and
// §7.16's rule that a sum never prints without its window binds here exactly as
// it bound the relay's. It is a count of the sessions that contributed a
// MEASURED reading, not of the vendor's sessions: a conversation that has not
// called a model yet, and one whose counts failed the adapter's self-check,
// both carry no model.Session.Tokens and are therefore in neither the sum nor
// the count. That is what makes "summed across 5 sessions on disk" exact rather
// than approximately true — it names the five, and never folds a sixth in as a
// zero.
//
// The consequence a reader has to be able to see is that this number can go
// DOWN. Deleting a conversation removes its counts from the next scan's sum,
// which a meter can never do; "on disk" is the word carrying that, and it
// survives every dress level below.
type scanSpend struct {
	in, out  int64
	sessions int
}

// usageBlocks assembles the fleet, in fleetOrder.
//
// Quota comes from quotaVendors — the SAME assembly the header uses, including
// its "transcript outranks relay" rule (§7.15). Two functions deciding which
// source speaks for a vendor is how the two surfaces would come to disagree
// about the same account, and a header and a detail view that contradict each
// other are worse than either alone.
//
// Spend comes from the sessions this scan read, and from nowhere else. It used
// to come from the hook relay's file as well (§7.16); the owner retired that
// DISPLAY on 2026-08-09 and the relay keeps running, so Snapshot.Spend is still
// filled by every scan and is deliberately read by nothing here.
func usageBlocks(st State) []usageBlock {
	quotas := map[model.VendorID]quotaVendorBlock{}
	for _, q := range quotaVendors(st) {
		quotas[q.vendor] = q
	}
	spend := scanSpendByVendor(st)
	sessions := map[model.VendorID]int{}
	for _, s := range st.Snap.Sessions {
		sessions[s.Vendor]++
	}

	seen := map[model.VendorID]bool{}
	var out []usageBlock
	add := func(v model.VendorID) {
		if seen[v] {
			return
		}
		seen[v] = true
		b := usageBlock{vendor: v, sessions: sessions[v]}
		if q, ok := quotas[v]; ok {
			q := q
			b.quota = &q
		}
		if t, ok := spend[v]; ok {
			t := t
			b.spend = &t
		}
		if b.quota == nil && b.spend == nil && b.sessions == 0 {
			return
		}
		out = append(out, b)
	}
	for _, v := range fleetOrder {
		add(v)
	}

	// A reading from a vendor fleetOrder does not name still gets a block,
	// sorted, after the ones it does. The quota relay files are written by
	// whatever statusline ran, and a cache entry telltale can read but cannot
	// place is a measurement — dropping it because the fleet table has not
	// caught up would be the view hiding data it has. A scan-derived total
	// cannot reach here from an unnamed vendor today, since it is summed from
	// sessions and fleetOrder names every vendor with an adapter; it is swept
	// anyway so the two sources cannot fall out of step later.
	var extra []model.VendorID
	for v := range quotas {
		if !seen[v] {
			extra = append(extra, v)
		}
	}
	for v := range spend {
		if !seen[v] {
			extra = append(extra, v)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i] < extra[j] })
	for _, v := range extra {
		add(v)
	}
	return out
}

// scanSpendByVendor sums the measured per-session token counts this scan read,
// one entry per vendor that produced at least one.
//
// It is arithmetic over measured integers and nothing else — no rate, no
// pricing, no projection — which is the whole of what §4a.1 allows a derived
// number to be here: addition of readings, presented with the count of readings
// it added. Every vendor is eligible; only agy writes token counts an adapter
// can read today (§3.8), so only agy has an entry, and a second vendor that
// starts reporting them arrives on this surface with no code change.
//
// A session with no Tokens contributes nothing and is not counted, and the
// difference between "no Tokens" and "Tokens that are zero" is doing real work:
// the first is a conversation that never called a model, or one whose read
// failed its self-check and said so in Diagnostics; the second would be a
// measured nothing. Only the second may sit in the sum.
func scanSpendByVendor(st State) map[model.VendorID]scanSpend {
	var out map[model.VendorID]scanSpend
	for _, s := range st.Snap.Sessions {
		if s == nil || s.Tokens == nil {
			continue
		}
		if out == nil {
			out = map[model.VendorID]scanSpend{}
		}
		t := out[s.Vendor]
		t.in += s.Tokens.Input
		t.out += s.Tokens.Output
		t.sessions++
		out[s.Vendor] = t
	}
	return out
}

// usageLines renders the body.
func usageLines(st State, sty Styles, g Glyphs) []string {
	cells := usageGaugeFor(st.Width)
	out := []string{usageHeading(st, sty, g)}

	blocks := usageBlocks(st)
	if len(blocks) == 0 {
		// Not a table of zeroes and not a blank body. "Nothing to show" is a
		// measurement here — it says every vendor telltale watched had no
		// session, no reading and no total — and §7.7's rule is that an empty
		// state says which emptiness it is in words.
		return append(out, "", strings.Repeat(" ", usageIndent)+
			sty.Muted.Render("no vendor on this machine has reported a quota reading or a token count"))
	}
	for _, b := range blocks {
		out = append(out, "")
		out = append(out, usageBlockLines(st, b, cells, sty, g)...)
	}
	return out
}

// usageHeadingNotes states the view's organizing distinction, in three dress
// levels. It sheds like every other line in this product — decoration first,
// and the surface's own name never — because a legend that pushes past the
// frame is worse than no legend.
var usageHeadingNotes = []string{
	"quota is a reading against a limit; spend is a count with none",
	"quota has a limit; spend has none",
	"",
}

func usageHeading(st State, sty Styles, g Glyphs) string {
	return usageFit(" "+sty.Text.Render("fleet usage"), usageHeadingNotes, st.Width, sty, g)
}

// usageBlockLines renders one vendor: a heading that states the QUOTA seam, and
// then a line per fact underneath it.
//
// The heading always speaks about quota — where the reading came from, or why
// there is none — and never about spend. That asymmetry is deliberate and it is
// what makes a spend-bearing vendor legible: agy's block would otherwise show a
// relayed percentage and a token total with nothing saying they came from two
// different seams. The spend line explains itself in its own vocabulary (a verb
// and a window, §7.16) and needs no heading; the absence of a quota reading
// explains nothing at all unless something says it out loud.
//
// A vendor with neither therefore falls out of the same code path as one line
// with no facts under it, rather than needing a case of its own.
func usageBlockLines(st State, b usageBlock, cells int, sty Styles, g Glyphs) []string {
	head := " " + sty.Identity.Render(string(b.vendor))

	var lines []string
	if b.quota != nil {
		head = usageFit(head, usageSourceDress(*b.quota, g), st.Width, sty, g)
		lines = append(lines, usageQuotaLines(st, *b.quota, cells, sty, g)...)
	} else {
		head = usageFit(head, usageQuotaAbsence(b.vendor, g), st.Width, sty, g)
	}
	if b.spend != nil {
		lines = append(lines, usageSpendLines(st, *b.spend, sty, g)...)
	}
	return append([]string{head}, lines...)
}

// usageFit appends the first dress level that fits beside a heading, and
// truncates the last one rather than dropping it.
//
// Dropping the last level would leave a vendor name standing alone with no
// statement attached, which reads as a rendering bug rather than as a fact —
// the footer's rule for its last surviving notice, applied here for the same
// reason.
func usageFit(head string, dress []string, width int, sty Styles, g Glyphs) string {
	for i, d := range dress {
		if d == "" {
			return head
		}
		if lipgloss.Width(head)+2+lipgloss.Width(d) <= width-1 {
			return head + "  " + sty.Muted.Render(d)
		}
		if i == len(dress)-1 {
			room := width - 1 - lipgloss.Width(head) - 2
			if room < 1 {
				return head
			}
			return head + "  " + sty.Muted.Render(truncate(d, room, g.Ellipsis))
		}
	}
	return head
}

// usageSourceDress names where a vendor's quota reading came from, most dressed
// first.
//
// The source is not trivia. §7.15 makes it load-bearing: a transcript-sourced
// block is re-measured every scan, while a relayed one is exactly as old as the
// last statusline render, and only the first may ever carry a burn forecast. A
// reader deciding how much to trust a percentage needs to know which of the two
// they are looking at.
//
// The AGE survives every level, including the barest. Shedding it would
// re-present a stale number as fresh, which is the one thing §7.15 says the
// relay may never do — so the phrase around the age is what gives way.
func usageSourceDress(b quotaVendorBlock, g Glyphs) []string {
	if !b.relayed {
		return []string{"quota read from its own store, this scan", "scan-fresh", ""}
	}
	age := ""
	if b.age >= quotaAgeShown {
		age = " " + g.Mid + " " + theme.Age(b.age) + " ago"
	}
	return []string{
		"quota relayed by the statusline" + age,
		"relayed" + age,
		strings.TrimPrefix(age, " "),
	}
}

// usageQuotaAbsence is why a vendor has no quota reading, and it keeps the
// three kinds of nothing apart (§4a.1). Two of them render; the third collapses
// into one of those two, deliberately, and §7.17 records that as a limitation.
//
//   - STRUCTURALLY ABSENT — Gemini, Cursor and grok have no account quota
//     anywhere a passive reader can see, relay or not (§7.15). For Cursor that
//     verdict was re-measured on 2026-08-08 and came back harder: the only
//     account figures on its disk are Statsig experiment values stamped
//     `is_user_in_experiment:false`, never consumption (§7.16). grok's is
//     §3.9a's, measured the same way and 2026-08-09. Nothing the user can do
//     changes any of them, so the line names the seam rather than an action —
//     telling someone to go and enable a thing that does not exist is worse
//     than telling them nothing.
//   - SEAM EXISTS, NEVER SEEN — Claude's and agy's quota reaches disk only when
//     `telltale statusline` renders it and writes the relay entry (§7.15). This
//     line names the statusline, because that IS the action: the reading turns
//     up as soon as the gauge runs in that vendor.
//   - AGED OUT — a relayed entry whose reset has passed, or which is over 24h
//     old, is dropped by quotacache's reader before the HUD ever sees it
//     (§7.15's self-expiry). It is therefore indistinguishable here from
//     never-seen, and it renders as never-seen. Keeping the expired numbers
//     around so this view could tell the two apart would mean holding a
//     percentage §7.15 calls not stale but FALSE, which is a strictly worse
//     trade than losing one distinction.
//
// Codex is in none of the three: its quota comes from its own store, so an
// absence there is a statement about what this scan read rather than about a
// relay that never fired, and borrowing either of the other two sentences for
// it would name the wrong seam.
// The separator is g.Mid rather than a literal "·" for the reason the whole
// glyph table exists: --ascii is a code-page switch, not a colour one, and a
// middle dot baked into a sentence is mojibake on the console the ASCII set is
// for. This is the one place in this file where a literal would have survived
// every test that renders the Unicode set.
func usageQuotaAbsence(v model.VendorID, g Glyphs) []string {
	mid := " " + g.Mid + " "
	switch v {
	case model.VendorClaude, model.VendorAntigravity:
		return []string{
			"no quota relayed yet" + mid + "the telltale statusline writes it",
			"no quota relayed yet",
		}
	case model.VendorCodex:
		return []string{"no quota in the sessions read this scan"}
	case model.VendorGemini:
		return []string{"no quota reaches disk anywhere telltale can read", "no quota anywhere"}
	case model.VendorCursor:
		return []string{
			"no quota anywhere" + mid + "its store holds experiment values, not usage",
			"no quota anywhere",
		}
	case model.VendorGrok:
		// The measured sentence, from §3.9a's survey rather than from the
		// vendor's docs: a regex for rate/limit/quota across the whole store
		// matched tool-configuration keys and nothing else, so no window, no
		// ordinal and no reset time reaches disk at all.
		//
		// This vendor is also the one place the block's quota-only rule needed
		// defending rather than merely following. grok is the first vendor that
		// writes MONEY down (§3.9a), so it is the one a reader might expect a
		// spend line under — and it gets none, because the only figure on its
		// disk is a per-TURN cost with no session or account total anywhere,
		// which the detail pane already labels as the turn's. Summing a tail of
		// an 818 KB append-only file would be a lower bound wearing a total's
		// clothes. The absence is therefore the honest render, and the reasoning
		// lives in §7.17 rather than on this line: the heading speaks about
		// quota and never about spend, and buying grok an exception would cost
		// every other block its meaning.
		return []string{
			"no quota anywhere" + mid + "no window, no ordinal, no reset time on its disk",
			"no quota anywhere",
		}
	default:
		return []string{"no quota telltale can read"}
	}
}

// usageQuotaLines renders one line per window: label, gauge, percent,
// countdown.
//
// A window the vendor declared but has no figure for renders the absent marker
// and NO bar — never 0%. That is §7.1 principle 1 on the surface whose whole
// job is to report quota, and it is the difference between "this account has
// used none of its allowance" and "we have no reading".
func usageQuotaLines(st State, b quotaVendorBlock, cells int, sty Styles, g Glyphs) []string {
	out := make([]string, 0, len(b.windows))
	for i := range b.windows {
		w := b.windows[i]
		var v strings.Builder
		if cells > 0 {
			v.WriteString(gauge(w.UsedPercent, cells, g, sty))
			v.WriteString(" ")
		}
		if w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100 {
			p := float64(*w.UsedPercent)
			// Severity colour, redundant with the number beside it as always
			// (§7.1 rule 2) — the percentage is the carrier and the hue only
			// makes it findable.
			v.WriteString(sty.Sev(p).Render(padLeft(theme.Percent(p), usagePct, g)))
		} else {
			v.WriteString(sty.Absent().Render(padLeft(g.Absent, usagePct, g)))
		}
		if w.ResetsAt != nil {
			if d := w.ResetsAt.Sub(st.Now); d > 0 {
				// A space between the glyph and the digits, the same dogfood
				// finding the header's countdown carries: ↻ renders at
				// ambiguous width and reads as one garbled token when glued on.
				v.WriteString("  " + sty.Muted.Render(g.Reset+" "+theme.Countdown(d)))
			}
		}
		out = append(out, usageRow(w.Label, v.String(), sty, g))
	}
	return out
}

// usageSpendWindows is the shed cascade for the accumulation window, most
// dressed first, and every level is a complete statement of the same claim.
//
// Nothing here is optional decoration, which is why this cascade drops WORDS
// and never facts. §7.16's rule — a sum is only honest while it says what it is
// a sum of — means the count and the "on disk" both have to reach the narrowest
// terminal telltale renders on:
//
//   - the COUNT, because it is the window. Without it the line is a token
//     figure with no scope, the "48k pretending to be a state" §7.16 forbids.
//   - "on disk", because it is the difference between this sum and the meter
//     Cursor's used to be. A total summed over what is on disk right now goes
//     DOWN when a conversation is deleted, and a reader who has been told only
//     "across 5 sessions" has been given a number they will reasonably expect
//     to be monotonic.
//
// "summed" and "this scan" are the two words that can go, in that order: the
// first is implied by the arithmetic being on screen, and the second by the
// view being live.
var usageSpendWindows = []string{
	"summed across %s on disk, this scan",
	"summed across %s on disk",
	"across %s on disk",
	"%s on disk",
}

// usageSpendLines renders the vendor's measured token consumption under it —
// one row where the whole claim fits on one, and two where it does not.
//
// The verb is the LABEL here rather than a word beside the vendor's name, which
// is the one thing this surface changes about §7.16's rendering — and it makes
// the claim stronger rather than weaker, because "spent" now sits in the column
// the quota windows label themselves in, so the two readings are visibly the
// same kind of statement about the same vendor and visibly different numbers.
//
// Everything §7.16 forbids stays forbidden: no gauge, no percentage, no
// countdown, and the accumulation window travels at every dress level.
//
// "uncached in" rather than "in" is not a nicety. agy's adapter reads the
// vendor's UNCACHED input count (§3.8's `#1.#4.#2`); the cache-read component
// sits at a field number the survey marks lower-confidence and the adapter
// refuses to fold in. Labelling the number "in" would quietly promote a partial
// figure to a whole one, and this line is the one place a reader would have no
// way to catch that.
//
// # Why this one thing wraps when nothing else on this surface does
//
// Every other line here sheds: it drops decoration until the facts fit, which
// works because every other line HAS decoration — a gauge that re-states a
// number still on screen, a phrase around an age. This line is facts only. Two
// token counts, a label that has to say "uncached", and a window §7.16 forbids
// printing the sum without: at 60 columns those do not fit on one row and there
// is nothing left to spend.
//
// The header solved that problem by dropping whole vendor blocks, because a
// header has a hard budget of one or two lines. This is a BODY, and it scrolls
// (§7.17) — so it can pay a second row, and a second row costs nothing a reader
// values next to a sum that has stopped saying what it summed. The window moves
// down, hanging under the counts in the same indent, and it re-runs its own
// cascade there: a row to itself buys back the dress the shared row could not
// afford, so the narrow render says MORE about the window than the cramped
// single-line one would have.
//
// It carries no leading glyph to mark the continuation. The mid dot separates
// facts on a line everywhere else in this product, and giving it a second job
// here is the failure §9.26 is a whole section about.
func usageSpendLines(st State, sp scanSpend, sty Styles, g Glyphs) []string {
	counts := sty.Muted.Render("uncached in") + " " + sty.Text.Render(theme.Tokens(sp.in)) +
		" " + sty.Muted.Render(g.Mid) + " " +
		sty.Muted.Render("out") + " " + sty.Text.Render(theme.Tokens(sp.out))
	windows := usageSpendWindowLevels(sp.sessions)

	for _, w := range windows {
		line := usageRow("spent", counts+"  "+sty.Muted.Render(g.Mid+" "+w), sty, g)
		if lipgloss.Width(line) <= st.Width-1 {
			return []string{line}
		}
	}

	out := []string{usageRow("spent", counts, sty, g)}
	for i, w := range windows {
		// The label column is empty on the continuation: "spent" is a verb and
		// this row is not a second thing that was spent.
		cont := usageRow("", sty.Muted.Render(w), sty, g)
		if lipgloss.Width(cont) <= st.Width-1 || i == len(windows)-1 {
			return append(out, cont)
		}
	}
	return out
}

// usageSpendWindowLevels renders the window's cascade for a session count, most
// dressed first.
func usageSpendWindowLevels(sessions int) []string {
	out := make([]string, 0, len(usageSpendWindows))
	for _, w := range usageSpendWindows {
		out = append(out, fmt.Sprintf(w, plural(sessions, "session", "sessions")))
	}
	return out
}

// plural renders a count with the noun that agrees with it. One conversation is
// a window too, and "1 sessions on disk" reads as a rendering bug in a view
// whose entire argument is that it is careful.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// usageRow places one labelled fact under a vendor heading.
func usageRow(label, value string, sty Styles, g Glyphs) string {
	return strings.Repeat(" ", usageIndent) +
		sty.Muted.Render(padRight(label, usageLabel, g)) +
		strings.Repeat(" ", usageGap) + value
}
