package hud

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// Freshness boundaries for the scan itself (design.md §7.7). Retained values
// are not "presented as fresh" at any of these, because the age of the
// measurement is on screen next to them — that is the condition the
// honest-gauge rule actually imposes.
const (
	staleAfter    = 3 * time.Second
	criticalAfter = 60 * time.Second

	// idleCutoff is what the "show all" toggle reveals.
	idleCutoff = 8 * time.Hour

	// spinAfter is how long the first scan must run before telltale admits to
	// being busy. Below it the spinner would be a flash, not information.
	spinAfter = 250 * time.Millisecond
)

// Render draws one frame.
//
// It is pure over State: it never calls time.Now, never touches the
// filesystem, and never consults the environment. State.Now is stamped when
// the tick arrives, the same discipline as statusline.Options.Now, and it is
// the reason every render in the design doc is testable without a terminal.
func Render(st State, sty Styles, g Glyphs) string {
	if st.Width < MinWidth {
		return fmt.Sprintf(" telltale needs %d columns (have %d)", MinWidth, st.Width)
	}
	if st.Height < MinHeight {
		return fmt.Sprintf(" telltale needs %d rows (have %d)", MinHeight, st.Height)
	}

	rows := visibleSessions(st)
	hasCtx, hasCost := columnsInUse(rows)
	lay := resolveLayout(st.Width, hasCtx, hasCost)

	stale := st.scanAge() > staleAfter && !st.Snap.At.IsZero()
	rowSty := sty.Dim(stale)

	quota := quotaBlock(st, sty.Dim(st.scanAge() > criticalAfter && !st.Snap.At.IsZero()), g, st.Width)
	header := headerLines(st, lay, quota, sty, g)

	full := st.Height >= fullChromeHeight
	// The column-header row names the grid, so it appears only when the body
	// IS the grid. Over a help overlay, a detail pane or an empty state it
	// would label columns that are not on screen.
	showColumns := full && !st.Help && !st.Detail && len(rows) > 0

	chrome := len(header) + 1 // + footer
	if full {
		chrome += 2 // the two rules
	}
	if showColumns {
		chrome++
	}
	bodyHeight := st.Height - chrome
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body []string
	isRows := false
	switch {
	case st.Help:
		body = helpLines(st, lay, hasCtx, hasCost, sty, g)
	case st.Detail:
		body = detailLines(st, rows, rowSty, g)
	case len(rows) == 0:
		body = emptyLines(st, sty, g)
	default:
		body = rowLines(st, rows, lay, rowSty, g)
		isRows = true
	}

	hiddenBelow := 0
	if len(body) > bodyHeight {
		start := st.Scroll
		if isRows && st.Cursor >= 0 {
			// The viewport follows the selection rather than the other way
			// round. Doing it here keeps every layout number in one place:
			// Update does not know how tall the chrome is this frame, and a
			// second copy of that arithmetic is a second thing to get wrong.
			if st.Cursor < start {
				start = st.Cursor
			}
			if st.Cursor > start+bodyHeight-1 {
				start = st.Cursor - bodyHeight + 1
			}
		}
		if start > len(body)-bodyHeight {
			start = len(body) - bodyHeight
		}
		if start < 0 {
			start = 0
		}
		if isRows {
			// "+N more" counts sessions. Reporting it for a clipped help
			// overlay would read as hidden rows.
			hiddenBelow = len(body) - bodyHeight - start
		}
		body = body[start : start+bodyHeight]
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}

	out := make([]string, 0, st.Height)
	out = append(out, header...)
	if full {
		out = append(out, rule(st.Width, sty, g))
	}
	if showColumns {
		out = append(out, columnHeader(lay, sty, g))
	}
	out = append(out, body...)
	if full {
		out = append(out, rule(st.Width, sty, g))
	}
	out = append(out, footerLine(st, len(rows), hiddenBelow, sty, g))
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- selection

// visibleSessions applies the vendor filter, the find query, the idle cutoff
// and the sort. Every one of those can hide a row, and every one of them is
// stated in the footer when it is not the default — a monitor that silently
// hides rows is a liar.
func visibleSessions(st State) []*model.Session {
	out := make([]*model.Session, 0, len(st.Snap.Sessions))
	for _, s := range st.Snap.Sessions {
		if !st.Filter.Accepts(s.Vendor) {
			continue
		}
		if !st.Matches(s) {
			continue
		}
		if !st.ShowAll {
			// A session with no activity timestamp is NOT hidden: "we have no
			// signal" is not evidence that it is old.
			if age, ok := s.Age(st.Now); ok && age > idleCutoff {
				continue
			}
		}
		out = append(out, s)
	}
	sortSessions(out, st.Sort, st.Now)
	return out
}

// sortSessions orders rows, always falling back to the session key so equal
// sort values hold a stable order frame to frame. A tie-break wobble is a
// moving element on a screen whose churn budget is one AGE cell (§7.1 rule 4).
func sortSessions(rows []*model.Session, key SortKey, now time.Time) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch key {
		case SortContext:
			ap, aok := percentOf(a.ContextPercent)
			bp, bok := percentOf(b.ContextPercent)
			if aok != bok {
				return aok
			}
			if aok && ap != bp {
				return ap > bp
			}
		case SortCost:
			ac, aok := costOf(a.Cost)
			bc, bok := costOf(b.Cost)
			if aok != bok {
				return aok
			}
			if aok && ac != bc {
				return ac > bc
			}
		default:
			aa, aok := a.Age(now)
			ba, bok := b.Age(now)
			if aok != bok {
				return aok
			}
			if aok && aa != ba {
				return aa < ba
			}
		}
		return a.Key() < b.Key()
	})
}

func percentOf(p *model.Percent) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return float64(*p), true
}

func costOf(c *model.USD) (float64, bool) {
	if c == nil {
		return 0, false
	}
	return float64(*c), true
}

// columnsInUse reports whether any visible row can fill the CONTEXT and COST
// columns. Neither is dropped for a vendor reason — it is dropped because
// every visible cell would be an em dash.
func columnsInUse(rows []*model.Session) (ctx, cost bool) {
	for _, s := range rows {
		if s.ContextPercent != nil {
			ctx = true
		}
		if s.Cost != nil {
			cost = true
		}
	}
	return ctx, cost
}

// ------------------------------------------------------------------- header

func headerLines(st State, lay Layout, quota string, sty Styles, g Glyphs) []string {
	left := headerIdentity(st, sty, g)

	if quota == "" {
		return []string{left}
	}
	if lay.Tier == TierWide && lipgloss.Width(left)+3+lipgloss.Width(quota) <= st.Width-1 {
		return []string{joinEnds(left, quota, st.Width)}
	}
	// Below the wide tier the quota block wraps to its own line rather than
	// competing with identity for the same row.
	return []string{left, joinEnds("", quota, st.Width)}
}

func headerIdentity(st State, sty Styles, g Glyphs) string {
	// Two spaces around the separator, not one: the header's job is to read as
	// three distinct facts, and a tight separator reads as one run-on phrase.
	sep := "  " + sty.Muted.Render(g.Sep) + "  "
	parts := []string{" " + sty.Text.Render("telltale")}

	if st.Snap.At.IsZero() && st.Scanning {
		// telltale may animate its own work. This is the only animation in the
		// product, and it reports telltale's own I/O, which telltale is
		// entitled to describe (§7.6).
		frame := g.Spinner[st.Spinner%len(g.Spinner)]
		parts = append(parts, sty.Muted.Render(frame+" scanning"))
		return strings.Join(parts, sep)
	}

	total := len(st.Snap.Sessions)
	visible := len(visibleSessions(st))
	count := fmt.Sprintf("%d sessions", total)
	if visible != total {
		// "2 of 4" so the headline can never contradict the per-vendor totals
		// beside it.
		count = fmt.Sprintf("%d of %d sessions", visible, total)
	}
	if total == 1 && visible == total {
		count = "1 session"
	}
	parts = append(parts, sty.Text.Render(count))

	if vc := vendorCounts(st, sty); vc != "" {
		parts = append(parts, vc)
	}
	return strings.Join(parts, sep)
}

func vendorCounts(st State, sty Styles) string {
	counts := map[model.VendorID]int{}
	for _, s := range st.Snap.Sessions {
		counts[s.Vendor]++
	}
	short := st.Width < compactBreak
	var out []string
	for _, v := range []model.VendorID{
		model.VendorClaude, model.VendorCodex, model.VendorGemini,
		model.VendorAntigravity, model.VendorCursor,
	} {
		if counts[v] == 0 {
			continue
		}
		name := string(v)
		if short {
			name = vendorTag(v)
			name = strings.ToLower(name)
		}
		out = append(out, sty.Muted.Render(fmt.Sprintf("%s %d", name, counts[v])))
	}
	return strings.Join(out, "  ")
}

// quotaAgeShown is when a relayed account reading starts carrying its age.
// Below it the statusline is firing often enough that "just now" would be
// noise; from it on, the age IS the honesty — a relayed percentage without
// its measurement time presents a possibly-hours-old reading as fresh, the
// exact claim the staleness constants above exist to prevent for scans.
const quotaAgeShown = 5 * time.Minute

// quotaVendorBlock is one vendor's account quota as the header speaks it.
type quotaVendorBlock struct {
	vendor  model.VendorID
	windows []model.QuotaWindow
	// age is how old a RELAYED reading is; zero for a transcript-sourced
	// block, whose freshness is the scan's and already governed by the
	// header-wide staleness dim.
	age     time.Duration
	relayed bool
	// forecasts marks the one block whose windows feed the burn sampler.
	// Relayed blocks never forecast: re-reading an unchanged cache file is
	// not a new observation, and window ids collide across vendors (both
	// Claude and Codex have a "seven_day"), so a cross-block Forecast call
	// would pin one vendor's projection to another's gauge.
	forecasts bool
}

// quotaVendors assembles the header's account-quota blocks from both sources:
// the most recently active quota-bearing session (Codex today — its store
// carries quota) and the statusline relay (§7.15 — vendors whose quota exists
// only on their statusline stdin). One block per vendor, transcript reading
// preferred when both exist (it is re-measured every scan; the relay is as
// old as the last statusline render), ordered by vendor id like the vendor
// table so the blocks cannot reshuffle between frames.
func quotaVendors(st State) []quotaVendorBlock {
	var out []quotaVendorBlock
	windows, source := accountQuotaSource(st)
	var srcVendor model.VendorID
	if i := strings.IndexByte(source, '/'); i > 0 {
		srcVendor = model.VendorID(source[:i])
	}
	if len(windows) > 0 {
		out = append(out, quotaVendorBlock{vendor: srcVendor, windows: windows, forecasts: true})
	}
	for _, a := range st.Snap.Account {
		if a.Vendor == srcVendor {
			continue
		}
		age := st.Now.Sub(a.WrittenAt)
		if age < 0 {
			age = 0
		}
		out = append(out, quotaVendorBlock{vendor: a.Vendor, windows: a.Windows, age: age, relayed: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].vendor < out[j].vendor })
	return out
}

// quotaDress is one level of the quota line's shed cascade.
type quotaDress struct {
	gauges     bool
	countdowns bool
	forecasts  bool
	fullNames  bool
}

// quotaDressLevels is the shed order, most dressed first. What sheds is
// decoration and what never sheds is fact, in the grid's own cascade
// grammar (COST goes, then the gauge, the number never does): forecasts
// first (a derived projection), then full vendor names down to the
// established two-letter tags, then the bars (the percentage beside each
// bar says the same thing), then countdowns. Names shed before gauges on
// purpose — the tag carries the same fact in two cells, while a gauge
// dropped is the product's glanceability gone. Every level keeps vendor,
// window label, reading, and — for relayed blocks — the reading's age,
// because shedding the age would re-present a stale number as fresh,
// which is worse than showing nothing.
var quotaDressLevels = []quotaDress{
	{gauges: true, countdowns: true, forecasts: true, fullNames: true},
	{gauges: true, countdowns: true, fullNames: true},
	{gauges: true, countdowns: true},
	{countdowns: true},
	{},
}

// quotaBlock renders account-level quota, one labelled block per vendor that
// has a reading.
//
// rate_limits is a property of the account, not of the session: repeating it
// per row would assert per-session quota, which is false (§7.1). A vendor
// with no sourceable quota is absent — not zeroed — so the block count is
// itself a measurement: it shows exactly as many vendors as telltale can
// honestly speak for.
//
// The line tries each dress level in order and renders the first that fits.
// If even the barest level overflows, whole trailing blocks are dropped and
// the ellipsis says so — the footer's fitNotices rule: dropping is never
// silent, and half a vendor's quota is a worse claim than a marked absence.
func quotaBlock(st State, sty Styles, g Glyphs, width int) string {
	blocks := quotaVendors(st)
	if len(blocks) == 0 {
		return ""
	}
	var line string
	for _, d := range quotaDressLevels {
		line = renderQuotaLine(blocks, d, st, sty, g)
		if lipgloss.Width(line) <= width {
			return line
		}
	}
	for len(blocks) > 1 {
		blocks = blocks[:len(blocks)-1]
		line = renderQuotaLine(blocks, quotaDress{}, st, sty, g) +
			" " + sty.Muted.Render(g.Ellipsis)
		if lipgloss.Width(line) <= width {
			return line
		}
	}
	return line
}

func renderQuotaLine(blocks []quotaVendorBlock, d quotaDress, st State, sty Styles, g Glyphs) string {
	parts := make([]string, 0, len(blocks))
	for i := range blocks {
		parts = append(parts, renderQuotaVendor(blocks[i], d, st, sty, g))
	}
	return strings.Join(parts, "  "+sty.Muted.Render(g.Sep)+"  ")
}

func renderQuotaVendor(b quotaVendorBlock, d quotaDress, st State, sty Styles, g Glyphs) string {
	name := string(b.vendor)
	if !d.fullNames {
		name = strings.ToLower(vendorTag(b.vendor))
	}
	cells := make([]string, 0, len(b.windows))
	for i := range b.windows {
		w := b.windows[i]
		cell := sty.Muted.Render(w.Label)
		hasReading := w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100
		if d.gauges {
			cell += " " + gauge(w.UsedPercent, quotaGauge, g, sty) + " "
			if hasReading {
				cell += sty.Sev(float64(*w.UsedPercent)).Render(padLeft(theme.Percent(float64(*w.UsedPercent)), 5, g))
			} else {
				cell += sty.Absent().Render(padLeft(g.Absent, 5, g))
			}
		} else {
			// No gauge, no fixed-width percent cell: below the gauge tier the
			// line is fighting for columns and the padding buys alignment
			// nothing here — blocks are variable-width already.
			if hasReading {
				cell += " " + sty.Sev(float64(*w.UsedPercent)).Render(theme.Percent(float64(*w.UsedPercent)))
			} else {
				cell += " " + sty.Absent().Render(g.Absent)
			}
		}
		if d.countdowns && w.ResetsAt != nil {
			if dur := w.ResetsAt.Sub(st.Now); dur > 0 {
				// A space between the glyph and the digits: fonts render ↻ at
				// ambiguous width, and glued to the countdown it reads as one
				// garbled token (dogfood finding, 2026-08-02).
				cell += " " + sty.Muted.Render(g.Reset+" "+theme.Countdown(dur))
			}
		}
		// The forecast only renders beside a current reading: a window that is
		// present without a usage figure THIS scan shows an em dash, and a
		// live-looking projection next to a dash asserts a trend for a value we
		// just said we don't have (review finding).
		if d.forecasts && b.forecasts && hasReading {
			if f, ok := st.Burn.Forecast(w.ID, st.Now); ok {
				cell += "  " + sty.Muted.Render(forecastText(f, st.Now, g))
			}
		}
		cells = append(cells, cell)
	}
	out := sty.Muted.Render(name) + " " + strings.Join(cells, "  ")
	if b.relayed && b.age >= quotaAgeShown {
		// The basis rule (§7.12): the scope of a claim travels with the
		// number. "6% · 2h ago" is a measurement with its time attached;
		// "6%" alone would be the relay presenting last night as now.
		out += " " + sty.Muted.Render(g.Mid+" "+theme.Age(b.age)+" ago")
	}
	return out
}

// accountQuota picks the windows the header block speaks for.
//
// It is a separate function because two callers must agree exactly: the
// renderer, and the burn sampler in Update. If the sampler ever read a
// different session's windows, the forecast would describe a quota the header
// is not showing — the same class of error as a per-row quota cell.
func accountQuota(st State) []model.QuotaWindow {
	windows, _ := accountQuotaSource(st)
	return windows
}

// accountQuotaSource is accountQuota plus the identity of the session whose
// snapshot supplied the windows. The burn sampler needs it: two sessions carry
// snapshots of the same account windows taken at different moments, and a
// series stitched across sources mixes measurement times (review finding).
// The renderer ignores the source; only the sampler resets on it.
func accountQuotaSource(st State) ([]model.QuotaWindow, string) {
	var best *model.Session
	for _, s := range st.Snap.Sessions {
		if !s.Has(model.FieldQuota) {
			continue
		}
		if best == nil {
			best = s
			continue
		}
		if s.LastActivity != nil && (best.LastActivity == nil || s.LastActivity.After(*best.LastActivity)) {
			best = s
		}
	}
	if best == nil {
		return nil, ""
	}
	return best.Quota, string(best.Vendor) + "/" + best.ID
}

// forecastText renders a burn-rate projection: "~15:41 · 18m basis".
//
// Three deliberate choices, all of them about what the string is allowed to
// claim:
//
//   - the leading "~" is the same estimate marker the CONTEXT column uses. The
//     number was computed by telltale from its own samples, not read from the
//     vendor, and ADR-001 requires that be visible.
//   - the basis travels WITH the number, always. "~15:41" alone is a
//     prediction; "~15:41 · 18m basis" is a measurement with its scope
//     attached, and it is the scope that lets a reader discount it.
//   - the clock is formatted in State.Now's location, not the machine's. That
//     keeps Render pure — it never consults the environment — and it is what
//     makes a golden reproducible on any CI runner.
func forecastText(f Forecast, now time.Time, g Glyphs) string {
	return "~" + f.At.In(now.Location()).Format("15:04") +
		" " + g.Mid + " " + theme.Age(f.Basis) + " basis"
}

// -------------------------------------------------------------------- rows

func columnHeader(lay Layout, sty Styles, g Glyphs) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", 8)) // pad, dot, gap, vendor, gap, sep, gap
	b.WriteString(sty.Muted.Render(padRight("SESSION", lay.Session, g)))
	b.WriteString("  ")
	b.WriteString(sty.Muted.Render(padRight("MODEL", modelWidth, g)))
	if lay.ShowCtx {
		if lay.Gauge > 0 {
			b.WriteString("  ")
			// Left-aligned across the gauge + percent block: the label names a
			// zone, and right-aligning it would read as a heading for the
			// number alone.
			b.WriteString(sty.Muted.Render(padRight("CONTEXT", lay.Gauge+1+pctWidth, g)))
		} else {
			b.WriteString(" ")
			b.WriteString(sty.Muted.Render(padLeft("CTX", pctWidth, g)))
		}
	}
	if lay.ShowCost {
		b.WriteString("  ")
		b.WriteString(sty.Muted.Render(padLeft("COST", costWidth, g)))
	}
	b.WriteString("   ") // gap, separator column, gap
	b.WriteString(sty.Muted.Render(padLeft("AGE", ageWidth, g)))
	return b.String()
}

func rowLines(st State, rows []*model.Session, lay Layout, sty Styles, g Glyphs) []string {
	out := make([]string, 0, len(rows))
	for i, s := range rows {
		out = append(out, renderRow(st, s, lay, sty, g, i == st.Cursor))
	}
	return out
}

func renderRow(st State, s *model.Session, lay Layout, sty Styles, g Glyphs, selected bool) string {
	var b strings.Builder
	// The selection mark lives in the leading pad column, which was already
	// blank — selection costs the grid no width. It is a GLYPH, not a
	// highlight: §7.1 rule 2 says every distinction is carried by a glyph or a
	// number first, and a reverse-video row is a colour-only distinction that
	// vanishes under NO_COLOR.
	if selected {
		b.WriteString(sty.Text.Render(g.Cursor))
	} else {
		b.WriteString(" ")
	}
	b.WriteString(livenessDot(s, st, sty, g))
	b.WriteString(" ")
	b.WriteString(sty.Identity.Render(vendorTag(s.Vendor)))
	b.WriteString(" ")
	b.WriteString(sty.Muted.Render(g.Sep))
	b.WriteString(" ")
	b.WriteString(sty.Text.Render(padRight(sessionLabel(s, lay.Session, g), lay.Session, g)))
	b.WriteString("  ")
	b.WriteString(sty.Identity.Render(padRight(DisplayModel(s.Model), modelWidth, g)))

	if lay.ShowCtx {
		if lay.Gauge > 0 {
			b.WriteString("  ")
			b.WriteString(gauge(s.ContextPercent, lay.Gauge, g, sty))
		}
		b.WriteString(" ")
		b.WriteString(percentCell(s, sty, g))
	}
	if lay.ShowCost {
		b.WriteString("  ")
		if s.Cost != nil {
			b.WriteString(sty.Text.Render(padLeft(theme.Cost(float64(*s.Cost)), costWidth, g)))
		} else {
			b.WriteString(sty.Absent().Render(padLeft(g.Absent, costWidth, g)))
		}
	}

	b.WriteString(" ")
	b.WriteString(sty.Muted.Render(g.Sep))
	b.WriteString(" ")
	b.WriteString(ageCell(s, st.Now, sty, g))
	return b.String()
}

// livenessDot encodes state by GLYPH and INTENSITY, never by hue: green
// already means "under 60%", and one hue meaning two things is how a colour
// system rots.
//
// Unknown renders blank. It is a real state, and it is never rendered as
// "stale" — stale is a claim, and "we have no activity signal" is not.
func livenessDot(s *model.Session, st State, sty Styles, g Glyphs) string {
	switch s.Liveness(st.Now, st.Thresholds) {
	case model.LivenessLive:
		return sty.Text.Render(g.DotLive)
	case model.LivenessIdle:
		return sty.Text.Render(g.DotIdle)
	case model.LivenessStale:
		return sty.Muted.Render(g.DotStale)
	default:
		return " "
	}
}

func vendorTag(v model.VendorID) string {
	switch v {
	case model.VendorClaude:
		return "CC"
	case model.VendorCodex:
		return "CX"
	case model.VendorGemini:
		return "GE"
	case model.VendorAntigravity:
		return "AG"
	case model.VendorCursor:
		return "CU"
	default:
		s := strings.ToUpper(string(v))
		if len(s) > 2 {
			s = s[:2]
		}
		return s
	}
}

// sessionLabel is the row's identity: the session's own name if the vendor has
// one, else the workspace basename, else the vendor session id — followed by
// the sub-agent chip when the session is fanning out, and then the parent
// directory if there is still room.
//
// The chip's width is reserved BEFORE the name is truncated. A chip that
// disappears on a long project name would make the same session look like a
// different kind of session at a different terminal width, which is a lie by
// omission; the name is the field that can afford to lose a character.
//
// The parent directory is appended only when at least 14 cells remain free. It
// disambiguates same-named projects under different roots and stops the wide
// tier from opening a dead gulf between the name and the model, and it drops
// out automatically as the terminal narrows.
func sessionLabel(s *model.Session, width int, g Glyphs) string {
	chip := subagentChip(s, g)
	budget := width
	if chip != "" {
		budget -= lipgloss.Width(chip) + 1
	}
	if budget < 1 {
		// No room for a name beside the chip. The chip is the smaller claim
		// and the one that cannot be reconstructed from anything else, so at
		// this width the name goes and the chip stays.
		return truncate(chip, width, g.Ellipsis)
	}

	label := ""
	if s.Name != nil {
		label = sanitize(*s.Name)
	}
	if label == "" {
		if w, ok := s.WorkspaceName(); ok {
			label = sanitize(w)
		}
	}
	if label == "" {
		label = s.ID
	}
	label = truncate(label, budget, g.Ellipsis)
	if chip != "" {
		label += " " + chip
	}

	remaining := width - lipgloss.Width(label)
	if remaining < 14 || s.WorkspaceDir == nil {
		return label
	}
	parent := parentDir(sanitize(*s.WorkspaceDir))
	if parent == "" {
		return label
	}
	return label + "  " + elideLeft(parent, remaining-2, g.Ellipsis)
}

// subagentChip is the fan-out marker: "⑂~2" on a session with two recently
// written sub-agent transcripts.
//
// The estimate marker is not decoration. The COUNT is exact — telltale listed
// the directory — but "these are running right now" is an inference drawn from
// a recency boundary, and ADR-001 requires the inferred part be visible. A
// count of zero draws nothing at all: the absence of a chip is not a claim,
// and a "⑂0" on every Claude row would be noise asserting a fact nobody asked
// for. Zero is still stated in the detail pane, where there is room to say
// "measured zero" instead of leaving it to be inferred.
func subagentChip(s *model.Session, g Glyphs) string {
	if s == nil || s.Subagents == nil || *s.Subagents <= 0 {
		return ""
	}
	mark := ""
	if s.Derived.Has(model.FieldSubagents) {
		mark = "~"
	}
	return g.Fork + mark + strconv.Itoa(*s.Subagents)
}

// parentDir is display-only string handling, not path manipulation: a session
// can be read from a fixture recorded on another OS, so both separators are
// recognized on every platform.
func parentDir(dir string) string {
	dir = strings.TrimRight(dir, `/\`)
	i := strings.LastIndexAny(dir, `/\`)
	if i <= 0 {
		return ""
	}
	return dir[:i]
}

// percentCell renders the context number. A DERIVED value carries the estimate
// marker: it was computed by the adapter, not reported by the vendor, and
// ADR-001 requires that difference be visible rather than silently mixed in.
func percentCell(s *model.Session, sty Styles, g Glyphs) string {
	if s.ContextPercent == nil {
		return sty.Absent().Render(padLeft(g.Absent, pctWidth, g))
	}
	p := float64(*s.ContextPercent)
	// Out-of-range from a non-conforming adapter renders absent, matching
	// gauge(): the number cell must not state a reading the bar refuses.
	if p < 0 || p > 100 {
		return sty.Absent().Render(padLeft(g.Absent, pctWidth, g))
	}
	txt := theme.Percent(p)
	if s.Derived.Has(model.FieldContextPercent) {
		txt = "~" + txt
	}
	return sty.Sev(p).Render(padLeft(txt, pctWidth, g))
}

// ageCell renders the row's age, or absence.
//
// There is no zero here and no negative: a session with no readable activity
// timestamp renders the absent marker. "0s" would claim the session was active
// this instant, which is exactly the clock-skew failure the adapters degrade
// rather than clamp.
func ageCell(s *model.Session, now time.Time, sty Styles, g Glyphs) string {
	age, ok := s.Age(now)
	if !ok {
		return sty.Absent().Render(padLeft(g.Absent, ageWidth, g))
	}
	return sty.Text.Render(padLeft(theme.Age(age), ageWidth, g))
}

// ------------------------------------------------------------ empty & help

// emptyLines distinguishes "watching, found nothing" from "vendor not
// installed". Two different facts, two different words, never a fake row and
// never an error dialog.
func emptyLines(st State, sty Styles, g Glyphs) []string {
	head := "no active sessions"
	// Naming the thing that emptied the list is the point: "no active
	// sessions" when a query is hiding four of them is the monitor lying by
	// omission. The query wins the sentence because it is the more recent and
	// more surprising of the two narrowings.
	switch {
	case st.Query != "" && len(st.Snap.Sessions) > 0:
		head = `no sessions matching "` +
			truncate(sanitizeKeepingSpace(st.Query), st.Width/2, g.Ellipsis) + `"`
	case st.Filter != FilterAll && len(st.Snap.Sessions) > 0:
		head = "no " + st.Filter.String() + " sessions"
	}

	statusW, rootW := 0, 0
	for _, v := range st.Snap.Vendors {
		if n := len(v.Status.String()); n > statusW {
			statusW = n
		}
		if n := lipgloss.Width(string(v.Vendor)); n > rootW {
			rootW = n
		}
	}

	// centerBlock pads by at least one cell and never truncates, so every
	// column a vendor line spends past st.Width-1 tears the frame's right
	// edge — the empty-unreadable scenario rendered 74 columns in a 60-column
	// terminal before this budget existed. A frame that tears is the
	// honest-gauge rule failing at the layer below the numbers, so the line
	// gives way before the frame does. driftScope below already assumed this
	// exact minimum pad; `avail` makes the assumption a budget.
	avail := st.Width - 1

	var block []string
	for _, v := range st.Snap.Vendors {
		word := padRight(v.Status.String(), statusW, g)
		styled := sty.Muted.Render(word)
		if v.Status == StatusUnreadable || v.Status == StatusDrifted {
			// The two words that report a problem. Colour is the second signal
			// only: both are already legible as words under NO_COLOR, which is
			// the whole reason the fourth fact got a word rather than a tint on
			// "watching".
			styled = sty.SevWarn.Render(word)
		}
		// The root is truncated as a backstop only — every shipped adapter's
		// root is a short known path, but the budget has to hold for whatever
		// Discover reports, not for the roots we happen to have today.
		root := truncate(RedactHome(v.Root, st.Home), avail-(rootW+3+statusW+3), g.Ellipsis)
		line := sty.Identity.Render(padRight(string(v.Vendor), rootW, g)) +
			"   " + styled +
			"   " + sty.Muted.Render(root)
		// The slot after the root carries the status's own evidence: the
		// operating system's message for a store it refused, the scope of the
		// report for a store that no longer matches. Never both — Discover
		// either failed or it did not, and drift is only reachable when it did
		// not.
		switch {
		case v.Err != "":
			// Truncated, never dropped and never overflowing — the footer's
			// rule for its last surviving notice, for the same reason: an
			// ellipsis on a warning still tells the reader a warning is there,
			// while a dropped one leaves "unreadable" standing with no
			// evidence, and an untruncated one tears the frame.
			if room := avail - lipgloss.Width(line) - 2; room > 0 {
				line += sty.SevWarn.Render("  " + truncate(v.Err, room, g.Ellipsis))
			}
		case v.Status == StatusDrifted:
			// The scope is the status's RESOLUTION, not the status, so it is
			// the part that gives way when the line runs out of room — the same
			// cascade the grid uses when it sheds COST and then the gauge. The
			// word never goes, and the counts are still in the detail pane. The
			// budget assumes the minimum centring pad centerBlock will ever
			// apply, so whether this renders depends on this line alone and not
			// on how long some other vendor's root happens to be.
			//
			// It is shed whole, never truncated: half a count is a worse claim
			// than no count.
			if scope := driftScope(v); lipgloss.Width(line)+2+lipgloss.Width(scope) <= avail {
				line += sty.SevWarn.Render("  " + scope)
			}
		}
		block = append(block, line)
	}

	// The heading centres on its own so that a long OS error in the vendor
	// table below cannot shove it sideways between frames.
	out := centerBlock([]string{sty.Text.Render(head)}, st.Width)
	out = append(out, "")
	return append(out, centerBlock(block, st.Width)...)
}

// driftScope is the measurement behind the word: how many of the vendor's
// sessions reported drift, out of how many this scan read for it.
//
// The numerator is LABELLED, and that is the whole of why the phrasing is not
// the header's "n of m sessions". The two sentences look like the same kind of
// statement and are not: the header's is visible-of-total-across-every-vendor,
// this one is drifted-of-read-for-this-vendor — different numerator, different
// denominator, different population. Both land on screen together in the empty
// state (testdata/golden/empty-drifted.txt shows a header reading "0 of 2
// sessions" directly above this line), and in the borrowed grammar a reader
// parses the vendor line as "1 of codex's 2 sessions is showing" — which the
// header has just denied. Naming what the 1 counts is what makes the two
// unmistakable for each other.
func driftScope(v VendorView) string {
	return fmt.Sprintf("%d drifted of %d read", v.Drifted, v.Sessions)
}

// driftNotice is the vendor line folded onto one footer line.
//
// It exists because the vendor line renders in the empty state ONLY, and the
// empty state is very nearly the one screen drift cannot appear on: a vendor
// cannot drift without having produced sessions, so the grid it moved under is
// the grid that is showing. Without this the fourth VendorStatus word would be
// one nobody ever sees, and a store that silently stopped matching would go on
// reading as healthy at a glance — the exact failure internal/adapter/drift was
// built to catch and then only told the detail pane about.
//
// The footer rather than the rows, for two reasons. Drift is a fact about a
// STORE, and painting it onto rows would assert per-session what was measured
// per-vendor. And the grid deliberately renders degraded and absent cells
// identically (§4a.1) so that "we failed to read this" never starts to look
// like a value — a drift mark in a cell is that rule broken.
//
// It renders under every body — grid, empty state, help overlay, detail pane —
// rather than only where the vendor table is absent. A warning that comes and
// goes depending on which pane is open is one a reader cannot trust to be
// there, and the duplication in the empty state costs a line that is already
// saying the same thing in more detail.
//
// Up to two vendors are named, then only counted. Truncating a longer list
// would drop a drifted vendor from the one notice whose entire job is to name
// them, and the footer shares a fixed budget with every other notice; three
// vendors moving at once is a machine-level event that the count describes
// exactly, and the names are still on the vendor line and in the detail pane.
func driftNotice(st State, g Glyphs) string {
	var names []string
	for _, v := range st.Snap.Vendors {
		if v.Status == StatusDrifted {
			names = append(names, string(v.Vendor))
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1, 2:
		return g.Warn + " " + strings.Join(names, ", ") + " drifted"
	default:
		return fmt.Sprintf("%s %d vendors drifted", g.Warn, len(names))
	}
}

// arrowHint spells the cursor keys in whichever alphabet is in use.
func arrowHint(g Glyphs) string {
	if g.ASCII {
		return "up/down"
	}
	return "↑/↓" // ↑/↓
}

func helpLines(st State, lay Layout, hasCtx, hasCost bool, sty Styles, g Glyphs) []string {
	pad := strings.Repeat(" ", 8)
	keys := [][2]string{
		{"q", "quit  (also ctrl+c)"},
		{arrowHint(g), "move the selection  (also j / k)"},
		{"enter", "open the detail pane for the selected session"},
		{"/", "find: narrow rows by name or path"},
		{"esc", "close the pane, or cancel the find, or quit"},
		// The cycle separator is "> " and not "-> " for one reason: a fourth
		// vendor pushed this line past the 60-column floor, and the arrow was
		// the two cells per hop that bought nothing the chevron does not say.
		// TestNoLineExceedsTheTerminalWidth is what caught it.
		//
		// The SIXTH vendor exhausted that trick, so the cycle now wraps —
		// continued on an unkeyed line whose indent lands the hops under the
		// first one. Shortening the vendor names instead would have made the
		// overlay disagree with the word the footer prints, and an overlay
		// that teaches a name the product does not use is worse than a
		// two-line list.
		{"v", "vendor: all > claude > codex >"},
		{"", "        gemini > agy > cursor"},
		{"s", "sort: activity > context > cost"},
		{"a", "show all (include sessions idle > 8h)"},
		{"r", "rescan now"},
		{"?", "close this help"},
	}
	// The key column is padded to the widest key so the descriptions form one
	// left edge. Ragged descriptions read as a list of unrelated facts.
	keyW := 0
	for _, k := range keys {
		if w := lipgloss.Width(k[0]); w > keyW {
			keyW = w
		}
	}
	out := []string{""}
	for _, k := range keys {
		out = append(out, pad+sty.Identity.Render(padRight(k[0], keyW, g))+"  "+sty.Text.Render(k[1]))
	}

	// §7.2 requires the overlay to name any column auto-hidden this frame, and
	// why — otherwise a dropped column is indistinguishable from a bug.
	var hidden []string
	if lay.Tier >= TierCompact && !hasCtx {
		hidden = append(hidden, "CONTEXT")
	}
	if lay.Tier == TierWide && !hasCost {
		hidden = append(hidden, "COST")
	}
	if len(hidden) > 0 {
		out = append(out, "", pad+sty.Muted.Render(
			strings.Join(hidden, " and ")+": hidden, no visible session reports it"))
	}
	return out
}

func centerBlock(lines []string, width int) []string {
	max := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > max {
			max = w
		}
	}
	pad := (width - max) / 2
	if pad < 1 {
		pad = 1
	}
	out := make([]string, 0, len(lines))
	prefix := strings.Repeat(" ", pad)
	for _, l := range lines {
		if l == "" {
			out = append(out, "")
			continue
		}
		out = append(out, prefix+l)
	}
	return out
}

// ------------------------------------------------------------------ footer

func footerLine(st State, visible, hiddenBelow int, sty Styles, g Glyphs) string {
	// Find mode takes the whole footer. It is the product's only mode, so it
	// says so where the key hints normally live rather than letting an
	// unmodified keystroke quietly mean something new.
	if st.Finding {
		hint := "esc clear   enter apply"
		// The query is truncated to fit rather than allowed to push itself off
		// the line. joinEnds gives the right slot priority when both cannot
		// fit, so an over-long query would silently VANISH from the footer
		// while still hiding rows — the one thing this footer exists to
		// prevent. The ellipsis says there is more.
		budget := st.Width - 3 - lipgloss.Width(hint) - 2
		q := truncate(sanitizeKeepingSpace(st.Query), max(budget, 1), g.Ellipsis)
		return joinEnds(" "+sty.Text.Render("/"+q+g.Caret), sty.Muted.Render(hint), st.Width)
	}

	var keys string
	switch {
	case st.Help:
		keys = " " + sty.Muted.Render("? close")
	case st.Detail:
		keys = " " + sty.Muted.Render("esc close   "+arrowHint(g)+" session")
	default:
		// "r refresh" moved to the help overlay to make room. It is the
		// cheapest hint to lose: the HUD already rescans every second, so `r`
		// only ever shortens a wait, while `/` and `enter` are doors nobody
		// finds by accident.
		hints := []string{"q quit", "/ find", "enter detail", "v vendor", "s sort", "a all", "? keys"}
		switch tierFor(st.Width) {
		case TierNarrow:
			hints = []string{"q quit", "/ find", "? keys"}
		case TierCompact:
			hints = []string{"q quit", "/ find", "enter detail", "? keys"}
		}
		keys = " " + sty.Muted.Render(strings.Join(hints, "   "))
	}

	var notices []footerNotice
	if hiddenBelow > 0 {
		notices = append(notices, footerNotice{rankHiddenBelow,
			fmt.Sprintf("+%d more", hiddenBelow), sty.Muted})
	}
	if st.Query != "" {
		// The query survives leaving find mode, so it has to keep announcing
		// itself: an applied filter the user has forgotten about is the same
		// silent row-hiding the vendor filter notice exists to prevent.
		notices = append(notices, footerNotice{rankQuery,
			`find "` + truncate(sanitizeKeepingSpace(st.Query), 24, g.Ellipsis) + `"`, sty.Muted})
	}
	if st.Filter != FilterAll {
		// A monitor that silently hides rows is a liar: a non-default filter is
		// always stated.
		notices = append(notices, footerNotice{rankFilter,
			"filter " + st.Filter.String(), sty.Muted})
	}
	if st.Sort != SortActivity {
		notices = append(notices, footerNotice{rankSort, "sort " + st.Sort.String(), sty.Muted})
	}
	// The two ⚠ notices sit together, drift first: a stale scan resolves itself
	// on the next tick that succeeds, and a store that no longer matches does
	// not resolve at all until somebody goes and looks. The durable fact should
	// not be the one crowded out of a reader's attention by the transient one.
	if note := driftNotice(st, g); note != "" {
		notices = append(notices, footerNotice{rankDrift, note, sty.SevWarn})
	}
	if !st.Snap.At.IsZero() {
		if age := st.scanAge(); age > staleAfter {
			warn := sty.SevWarn
			if age > criticalAfter {
				warn = sty.SevCrit
			}
			msg := fmt.Sprintf("%s last scan %s ago", g.Warn, theme.Age(age))
			if st.Snap.Err != "" {
				msg += "   " + st.Snap.Err
			}
			notices = append(notices, footerNotice{rankStale, msg, warn})
		}
	}
	if len(notices) == 0 {
		return keys
	}

	// joinEnds has no truncation path — when the right side alone overruns the
	// line it returns it unclamped — so the notice block is fitted to the line
	// BEFORE it gets there. width-1 is the widest block joinEnds can place and
	// still leave its one column of left padding.
	kept, dropped := fitNotices(notices, st.Width-1, g)
	parts := make([]string, 0, len(kept))
	for _, n := range kept {
		parts = append(parts, n.sty.Render(n.text))
	}
	block := strings.Join(parts, noticeGap)
	if dropped {
		// The same ellipsis, meaning the same thing it means in every other
		// cell of this UI: there is more than fits. Muted, because it marks an
		// absence rather than stating a fact of its own.
		block = sty.Muted.Render(g.Ellipsis) + " " + block
	}
	return joinEnds(keys, block, st.Width)
}

// noticeGap separates two footer notices.
const noticeGap = "   "

// footerNotice is one footer notice, the style it renders in, and its rank.
//
// The text is kept PLAIN until the block is settled. Widths are measured and
// any last-resort truncation happens on it here, because truncating an
// already-styled string cuts through an ANSI escape sequence — the trap
// CLAUDE.md names, and one a golden rendered with PlainStyles is structurally
// blind to.
type footerNotice struct {
	rank int
	text string
	sty  lipgloss.Style
}

// Drop ranks for the footer notices. Lowest goes first when the line cannot
// hold them all.
//
// This is not a second priority rule; it is the one joinEnds already applies,
// asked one level down. joinEnds sacrifices the key hints because the notices
// carry facts the reader cannot get anywhere else on this screen, and the same
// question orders the notices among themselves: if this line cannot say it,
// where else would the reader find it out?
//
//   - sort hides nothing at all — it reorders, and the AGE column is right
//     there. It is the cheapest thing on the line to lose.
//   - "+N more" reports rows below the fold, on a row area the reader can see
//     is full, and one keypress reveals them.
//   - a filter and a query DO hide rows silently, which is what this footer
//     exists to refuse — but they are backstopped: headerIdentity prints
//     "N of M sessions" whenever anything narrows the list, so losing the
//     notice loses the CAUSE, never the fact that rows are hidden. The query
//     outranks the filter because a typed string is the more forgettable of
//     the two and the filter is one visible `v` press from being re-read.
//   - a stale scan is the machine's own problem and is recoverable from
//     nothing else on screen, but it re-announces itself every tick and clears
//     the moment a scan succeeds.
//   - drift is that same fact minus the self-clearing: it stays true, and
//     unsaid, until somebody goes and looks. It is the last notice to go.
const (
	rankSort = iota
	rankHiddenBelow
	rankFilter
	rankQuery
	rankStale
	rankDrift
)

// fitNotices drops notices, lowest rank first, until the block fits budget,
// and reports whether anything was dropped.
//
// It drops WHOLE notices. A block cut to length mid-notice would leave a
// half-rendered warning, which is worse than an absent one — and the caller
// marks the loss with an ellipsis, so a dropped notice is never a silent one.
func fitNotices(ns []footerNotice, budget int, g Glyphs) ([]footerNotice, bool) {
	dropped := false
	for len(ns) > 1 && noticeBlockWidth(ns, dropped, g) > budget {
		lowest := 0
		for i := range ns {
			if ns[i].rank < ns[lowest].rank {
				lowest = i
			}
		}
		ns = append(ns[:lowest:lowest], ns[lowest+1:]...)
		dropped = true
	}
	// A single notice that alone overruns the line is truncated rather than
	// dropped. An ellipsis on a warning still tells the reader a warning is
	// there; dropping the last one would leave a footer quietly claiming
	// nothing is wrong.
	if len(ns) == 1 {
		if room := budget - noticeMarkWidth(dropped, g); room > 0 &&
			lipgloss.Width(ns[0].text) > room {
			// Copied rather than written in place: ns may still be the caller's
			// own slice, and a fitting pass has no business editing it.
			only := ns[0]
			only.text = truncate(only.text, room, g.Ellipsis)
			return []footerNotice{only}, dropped
		}
	}
	return ns, dropped
}

func noticeBlockWidth(ns []footerNotice, dropped bool, g Glyphs) int {
	w := noticeMarkWidth(dropped, g)
	for i, n := range ns {
		if i > 0 {
			w += len(noticeGap)
		}
		w += lipgloss.Width(n.text)
	}
	return w
}

func noticeMarkWidth(dropped bool, g Glyphs) int {
	if !dropped {
		return 0
	}
	return lipgloss.Width(g.Ellipsis) + 1
}

// ------------------------------------------------------------------ pieces

func rule(width int, sty Styles, g Glyphs) string {
	return " " + sty.Rule().Render(strings.Repeat(g.Track, width-2))
}

// joinEnds places left at the start and right flush against width-1, leaving
// one column of padding on each side. When they cannot both fit, the right
// side wins: it carries the notices.
func joinEnds(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - 1 - lw - rw
	if gap < 1 {
		if rw+2 >= width {
			return " " + right
		}
		return strings.Repeat(" ", width-1-rw) + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// RedactHome replaces the user's home directory with the platform's
// environment-variable form. Vendor roots are the only paths the HUD renders,
// and on a shared machine a literal home path names its owner.
//
// home is passed in (resolved once at program start, State.Home) so this stays
// pure — Render never consults the environment. Empty home disables redaction.
func RedactHome(p, home string) string {
	if p == "" || home == "" {
		return p
	}
	clean := filepath.Clean(p)
	home = filepath.Clean(home)
	if !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(home)) {
		return p
	}
	// The prefix must end at a path boundary: C:\Users\sanleigh must not
	// match home C:\Users\sanle (review finding).
	if len(clean) > len(home) {
		if c := clean[len(home)]; c != '\\' && c != '/' {
			return p
		}
	}
	token := "~"
	if runtime.GOOS == "windows" {
		token = "%USERPROFILE%"
	}
	return token + clean[len(home):]
}

// DisplayModel normalizes a model identity for the MODEL column.
//
// A vendor-supplied display name always wins. Otherwise a Claude model id is
// reshaped into the form the statusline shows ("claude-sonnet-4-5" ->
// "Sonnet 4.5"), and anything that does not match the pattern is returned
// unchanged. This is display normalization of a sourced value, never a guess
// about which model is running: an unrecognized id renders as itself.
func DisplayModel(m *model.Model) string {
	name, ok := m.Name()
	if !ok {
		return ""
	}
	if m.DisplayName != "" {
		return sanitize(name)
	}
	return sanitize(normalizeModelID(name))
}

func normalizeModelID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) < 3 || parts[0] != "claude" {
		return id
	}
	// Any claude-<family>-<numeric…> id normalizes. A family allowlist would
	// leave the NEXT family name truncating raw in the MODEL cell — which is
	// exactly how claude-fable-5 rendered as "claude-fable…" (dogfood day 0).
	family := parts[1]
	if !allAlpha(family) {
		// Old-style ids put the version first (claude-3-5-sonnet): pattern
		// unknown, render the sourced id untouched rather than guess.
		return id
	}
	rest := parts[2:]
	// Drop a trailing release date: it is noise in a 13-column cell.
	if last := rest[len(rest)-1]; len(last) == 8 && allDigits(last) {
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 0 {
		return id
	}
	// Every remaining part must be numeric (a version), or the id has a shape
	// this function does not understand and must not restyle. The last part
	// may carry a bracketed variant suffix (claude-opus-5[1m] -> "Opus 5[1m]").
	for i, p := range rest {
		if i == len(rest)-1 {
			if b := strings.IndexByte(p, '['); b > 0 && strings.HasSuffix(p, "]") {
				p = p[:b]
			}
		}
		if !allDigits(p) {
			return id
		}
	}
	return strings.ToUpper(family[:1]) + family[1:] + " " + strings.Join(rest, ".")
}

func allAlpha(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
