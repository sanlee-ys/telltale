package hud

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
// A third kind of line joined them on 2026-08-09 and it belongs to NEITHER
// claim: the MODELS row names which models this scan actually saw working under
// a vendor. It is a census, not a reading — no limit, no total, nothing to
// compare it against — so it borrows neither vocabulary and simply lists what
// was there. It sits in the same label column as the other two, which is what
// makes the three legible as three kinds of statement about one vendor rather
// than as three layouts.
//
// The SPEND half was reshaped the same day, by the owner's ruling (§7.16 and
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

	// usageRuleMin is the shortest heavy rule the title line will draw. Below
	// it the rule stops reading as a rule and starts reading as a stray pair of
	// glyphs after a sentence, which is worse than no rule at all — the same
	// judgement the footer makes when it drops a notice rather than showing two
	// cells of one.
	usageRuleMin = 4
)

// quotaAgeWarn is where a relayed reading stops being merely OLD and starts
// being one a reader should not act on without re-running the vendor.
//
// The defect it answers is a field report (2026-08-09) and it is the sharpest
// possible statement of the problem: a nineteen-hour-old relayed reading of
// 15% rendered at full confidence beside a live gauge, while the account was
// actually at 44%. Nothing was dishonest — the age was on screen, `· 19h ago`,
// exactly as §7.15 requires — and it was still read as current, because a
// muted four-character suffix is the same weight as every other piece of
// chrome on the line. The age was PRESENT and it was not LOUD.
//
// **Five hours, and the number is argued rather than tuned.** It is the
// shortest quota window telltale has measured anywhere in the fleet — Claude's
// `five_hour` (§3.1) — and therefore the shortest span over which a vendor is
// known to reset a limit wholesale. A reading older than that has outlived the
// fastest-moving quota this product knows about: whatever window it reports,
// an entire window of the shortest kind could have opened and closed since it
// was taken, so the reader may no longer assume the number describes now.
// Below five hours the reading is old and still bounded by something; above it
// there is no window short enough to bound it.
//
// Three things it deliberately is NOT:
//
//   - It is not the per-block shortest window. `model.QuotaWindow` carries no
//     duration — only a label, a percentage and a reset time — so a per-block
//     threshold would have to INFER a length by parsing "5h" or "seven_day",
//     and a threshold derived from a display string is the class of guess §4a.1
//     rejects. Worse, it would have failed on the very reading that prompted
//     this: quotacache had already dropped Claude's expired 5h window, so the
//     surviving block reported only `7d` and a per-block rule would have stayed
//     silent at nineteen hours.
//   - It is not a second reason to DROP a reading. `quotacache` owns expiry —
//     a window whose reset has passed, or an entry past 24h, never reaches the
//     HUD (§7.15). This view renders everything it is given and changes only
//     how loudly. Dropping earlier than the reader does would hide a
//     measurement telltale holds.
//   - It is not a freshness gauge. There is no denominator for "how fresh" —
//     the same argument that keeps a bar off the spend line.
const quotaAgeWarn = 5 * time.Hour

// usageAgeReason is the WORD half of the escalation, and §7.1 rule 2 is why it
// exists: the hue is the second signal, so the sentence has to carry the claim
// on a monochrome console with no glyph set worth the name.
//
// It says "the fleet's" rather than "its" on purpose. Five hours is the
// shortest window in the FLEET, not necessarily in this block — an agy weekly
// reading six hours old has not outlived its own window, and a sentence
// claiming it had would trade one over-confident render for one over-stated
// warning.
const usageAgeReason = "older than the fleet's shortest quota window"

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

// usageLines renders the body, into a known number of rows.
//
// `room` is the body region's height, and it is a parameter rather than
// something this function could do without: the page's vertical rhythm is
// decided against it (usageAir). Every other body in the HUD renders at its
// natural length and lets Render pad the remainder, which is right for a grid
// — a list of rows ends where the rows end. It is wrong for a PAGE, and §7.17's
// field report (2026-08-09) is what the difference looks like: on a tall
// terminal the content stopped around 40% and the rest was blank, so a surface
// deliberately built to be READ trailed off into nothing like a truncated file.
func usageLines(st State, room int, sty Styles, g Glyphs) []string {
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

	body := make([][]string, 0, len(blocks))
	natural := len(out)
	for _, b := range blocks {
		lines := usageBlockLines(st, b, cells, sty, g)
		body = append(body, lines)
		natural += 1 + len(lines)
	}

	gap := usageAir(natural, room, len(blocks))
	for i, lines := range body {
		// The first block's single blank is the heading's own threshold and it
		// never grows: widening it would push the whole page down the screen,
		// which is vertical centring arriving by the back door. The §7.x anchor
		// rulings hold — the page starts where the page starts.
		n := gap
		if i == 0 {
			n = 1
		}
		for j := 0; j < n; j++ {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

// usageAir decides how many blank rows separate two vendor blocks: one, or two
// when the page has the rows to spare.
//
// **This reverses half of §7.17's air-and-alignment ruling, and the reversal is
// narrow.** §9.26's argument — a second weight is worth only what it is scarce
// — stands, and it is why the vendor headings still get no rule. The line it
// also produced, that "a one-row gap is the threshold and two rows is nothing
// the design asked for", was decided under a LINE BUDGET, on the assumption
// that a row spent on air is a row taken from a fact. On a tall terminal that
// assumption is simply false: the rows are there, unspent, and the choice is
// not air-versus-fact but air-versus-void.
//
// So the second row is never a design fiat and never a constant. It appears
// only when the page would otherwise leave MORE blank rows at the bottom than
// the widened gaps consume, which makes it a redistribution of air the page
// already has rather than an invention of any. On a short terminal the page is
// tight, exactly as before, and nothing about §7.17's shed behaviour moves.
//
// **It stops at two, and the cap is the whole argument for it being honest.**
// Distributing ALL the surplus — justifying the blocks down to the closing rule
// — was the obvious alternative and it is rejected on two counts. It makes gap
// height a function of terminal height and block count, so the distance between
// two vendors would encode nothing while looking like it encoded something; and
// it puts every gap on a variable, so one grok session appearing reshuffles the
// vertical position of every block below it, against §7.1 rule 4's one-cell
// churn budget. Air that says "these are separate things" has to be a constant
// to say it. Two rows say it twice as clearly as one and still say the same
// thing on every frame.
func usageAir(natural, room, blocks int) int {
	if blocks < 2 {
		return 1
	}
	if natural+blocks-1 > room {
		return 1
	}
	return 2
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

// usageHeading is the one line in the HUD that draws the heavy rule.
//
// The defect it fixes is §9.23's, one surface over: `fleet usage` and ` claude`
// started in the same column at the same weight, so the body's TITLE read as a
// peer of one of its entries — the outline whispering while its entries shout.
//
// The rule goes ON the title rather than under it, which is council's ruling
// rather than a preference: §9.11 spent a whole item removing a heading
// followed by a horizontal rule, on the finding that such a rule says nothing
// the heading had not, and ruled that a heading carries its own. It costs zero
// rows — which is what makes it affordable on a body with a line budget — and
// it cannot be confused with the frame, which is the light weight at full
// bleed one row above.
//
// The rule yields to the legend, never the other way round: the note is fitted
// first and the rule takes what is left, because a legend is a statement and a
// rule is chrome. At the 60-column floor that leaves ten cells, and if it ever
// left fewer than usageRuleMin the line simply has no rule.
//
// **The vendor headings deliberately get NO rule, not even the light one.**
// §9.26's whole argument is that a second weight is worth only as much as it is
// scarce, and a rule on every block would spend it fifteen times a screen to
// re-state what an indent, a blank row and the identity hue already say. Air is
// the boundary strength this body can afford (§9.11's ranking), and it already
// has exactly one row of it between blocks — the room's own threshold, and a
// second row is nothing the design asked for.
func usageHeading(st State, sty Styles, g Glyphs) string {
	head := usageFit(" "+sty.Text.Render("fleet usage"), usageHeadingNotes, st.Width, sty.Muted, g)
	room := st.Width - 1 - lipgloss.Width(head) - 2
	if room < usageRuleMin {
		return head
	}
	return head + "  " + sty.Rule().Render(strings.Repeat(g.RuleHeavy, room))
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
//
// The MODELS row comes first, before the quota windows and before spend, and
// the order is an argument rather than an accident: it names who did the work,
// and the rows under it say what that work cost — subject before predicate. It
// does not separate the heading from its own evidence, because the heading
// states a PROVENANCE ("relayed by the statusline") rather than a number; the
// windows are not its continuation, they are the next fact down.
func usageBlockLines(st State, b usageBlock, cells int, sty Styles, g Glyphs) []string {
	// The vendor NAME is the one place on this surface that spends a per-vendor
	// hue (§7.17, ratified 2026-08-09; the HUD half of council's §9.28). Six
	// blocks stack in one column here, so position answers nothing about which
	// vendor a paragraph belongs to — the condition council named for when a hue
	// earns its place. The dress beside it is NOT retinted: it is chrome, and
	// past quotaAgeWarn it is a warning that has to keep SevWarn.
	head := " " + sty.VendorIdentity(b.vendor).Render(string(b.vendor))

	if b.quota != nil {
		head = usageFit(head, usageSourceDress(*b.quota, g), st.Width, usageSourceStyle(*b.quota, sty), g)
	} else {
		head = usageFit(head, usageQuotaAbsence(b.vendor, g), st.Width, sty.Muted, g)
	}

	var lines []string
	if row := usageModelsRow(usageModelsSeen(st, b.vendor), st.Width, sty, g); row != "" {
		lines = append(lines, row)
	}
	if b.quota != nil {
		lines = append(lines, usageQuotaLines(st, *b.quota, cells, sty, g)...)
	}
	if b.spend != nil {
		lines = append(lines, usageSpendLines(st, *b.spend, sty, g)...)
	}
	return append([]string{head}, lines...)
}

// usageModelsSep joins two model names, and it is a comma rather than the g.Mid
// separator every other pair of facts on this surface uses. The distinction is
// real: `·` separates two DIFFERENT claims (a percentage from its age, a total
// from its window), and these are one claim with several members. A list joined
// by the fact separator reads as several readings.
const usageModelsSep = ", "

// usageModelsSeen is the model display names this scan actually observed for a
// vendor: deduped, and sorted rather than left in scan order.
//
// Three rules, and each of them is the honest-gauge rule wearing different
// clothes.
//
//   - **Only what is in this snapshot.** Never a remembered list, never the
//     vendor's catalogue. The row's claim is "these models did work on this
//     machine, and telltale saw it" — a name that survived from a previous scan
//     would be a claim about the past presented as the present, which is the
//     same defect the relay's age exists to prevent.
//   - **The grid's own normalization**, through DisplayModel, so `claude-opus-5`
//     is `Opus 5` in both places and a reader never has to work out that two
//     spellings are one model. A session whose adapter could not source a model
//     contributes nothing — absent renders absent, and a vendor where NO session
//     has a name gets no row at all rather than an em dash. An em dash would
//     claim telltale looked and found nothing nameable; the missing row claims
//     only that there is nothing here to say.
//   - **Sorted, not ranked.** Ordering by recency or by session count would make
//     the list reshuffle every time a turn lands, and §7.1 rule 4 budgets the
//     movement on this screen at one cell. Alphabetical is the ordering nothing
//     in the data can perturb.
func usageModelsSeen(st State, v model.VendorID) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range st.Snap.Sessions {
		if s.Vendor != v {
			continue
		}
		name := DisplayModel(s.Model)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// usageModelsRow lays the names into the shared fact column, and overflows
// honestly rather than clipping.
//
// The cap is the ROOM rather than a magic number, which is the same rule every
// other line on this surface follows: what fits is drawn, what does not is
// announced. `+2 more` is a count telltale measured, so the row never becomes a
// claim that two models were all there was — the failure mode of a list
// truncated with an ellipsis, where the reader cannot tell whether one name was
// dropped or nine. Names are never cut mid-word to squeeze another in; the
// marker's own width is reserved before the last name is accepted.
//
// The one exception is a terminal so narrow that even the FIRST name does not
// fit, where the name is truncated with the ellipsis and the marker still says
// how many are missing. That cannot happen at the 60-column floor (36 cells of
// room) and it is handled anyway, because "cannot happen" is how a grid shears.
func usageModelsRow(names []string, width int, sty Styles, g Glyphs) string {
	if len(names) == 0 {
		return ""
	}
	room := width - 1 - usageIndent - usageLabel - usageGap
	if room < 1 {
		return ""
	}

	text, kept := "", 0
	for i, n := range names {
		next := n
		if text != "" {
			next = text + usageModelsSep + n
		}
		// Reserve the marker's width whenever a name would be left over, so
		// accepting this one can never be what pushes the marker off the line.
		reserve := 0
		if i < len(names)-1 {
			reserve = lipgloss.Width(usageModelsMore(len(names) - i - 1))
		}
		if lipgloss.Width(next)+reserve > room {
			break
		}
		text, kept = next, i+1
	}
	if kept == 0 {
		marker := usageModelsMore(len(names) - 1)
		text = truncate(names[0], room-lipgloss.Width(marker), g.Ellipsis)
		if text == "" {
			return ""
		}
		kept = 1
	}

	// Identity, the token the grid already spends on a model name (§7.5) — the
	// same fact in the same hue on both surfaces, rather than a second colour
	// for a concept theme already has one for.
	value := sty.Identity.Render(text)
	if kept < len(names) {
		value += sty.Muted.Render(usageModelsMore(len(names) - kept))
	}
	return usageRow("models", value, sty, g)
}

func usageModelsMore(n int) string { return "  +" + strconv.Itoa(n) + " more" }

// usageFit appends the first dress level that fits beside a heading, and
// truncates the last one rather than dropping it.
//
// Dropping the last level would leave a vendor name standing alone with no
// statement attached, which reads as a rendering bug rather than as a fact —
// the footer's rule for its last surviving notice, applied here for the same
// reason.
//
// The style is a PARAMETER rather than `sty.Muted` inline, because one dress on
// this surface is not chrome: an over-age relayed reading renders its whole
// statement in the warning token (usageSourceStyle). Styling the dress whole,
// at the one point it is placed, is also what keeps the escape sequences out of
// the middle of the string — `truncate` walks runes and would happily cut
// through an ANSI sequence, and the goldens render PlainStyles and would never
// see it (CLAUDE.md's "ANSI trap").
func usageFit(head string, dress []string, width int, dressSty lipgloss.Style, g Glyphs) string {
	for i, d := range dress {
		if d == "" {
			return head
		}
		if lipgloss.Width(head)+2+lipgloss.Width(d) <= width-1 {
			return head + "  " + dressSty.Render(d)
		}
		if i == len(dress)-1 {
			room := width - 1 - lipgloss.Width(head) - 2
			if room < 1 {
				return head
			}
			return head + "  " + dressSty.Render(truncate(d, room, g.Ellipsis))
		}
	}
	return head
}

// usageSourceStyle is the hue half of the age escalation, and it is the SECOND
// signal — the dress text already carries the warning glyph and the reason in
// words, so this only makes it findable (§7.1 rule 2).
//
// The whole statement changes token rather than just the four characters of the
// age. Two reasons, and the first is the honest one: the sentence IS the
// warning once the reading is over-age — "quota relayed by the statusline"
// stops being provenance trivia and becomes the explanation of why a number on
// screen may be wrong. The second is mechanical, and it is CLAUDE.md's ANSI
// trap: a partially styled dress is a string with escapes in the middle, and
// the narrow-width path truncates dresses rune by rune.
//
// SevWarn on a sentence rather than on a value is the footer's own precedent —
// §7.5 gives the token "value ≥ 60; **warning notices**" — and the footer's
// stale-scan line already reads `⚠ last scan 1m ago` in exactly this token.
// There is deliberately no SevCrit step: §9.26's lesson is that a second level
// is worth what it is scarce, and the only boundary above this one that is not
// invented is quotacache's 24h drop, which is a disappearance rather than a
// warning.
func usageSourceStyle(b quotaVendorBlock, sty Styles) lipgloss.Style {
	if b.relayed && b.age >= quotaAgeWarn {
		return sty.SevWarn
	}
	return sty.Muted
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
//
// Past quotaAgeWarn the age escalates, and the escalation is carried by the
// text before it is carried by the hue: the warning glyph joins the age at
// every level including the barest, and the REASON rides the two dressed
// levels. That ordering is the shed grammar unchanged — the age is the fact and
// never sheds, the sentence explaining it is decoration and does. What the
// barest level loses is the argument, not the alarm.
func usageSourceDress(b quotaVendorBlock, g Glyphs) []string {
	if !b.relayed {
		return []string{"quota read from its own store, this scan", "scan-fresh", ""}
	}
	mid := " " + g.Mid + " "
	if b.age >= quotaAgeWarn {
		aged := mid + g.Warn + " " + theme.Age(b.age) + " ago" + mid + usageAgeReason
		return []string{
			"quota relayed by the statusline" + aged,
			"relayed" + aged,
			g.Warn + " " + theme.Age(b.age) + " ago",
		}
	}
	age := ""
	if b.age >= quotaAgeShown {
		age = mid + theme.Age(b.age) + " ago"
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
