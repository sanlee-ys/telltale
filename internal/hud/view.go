package hud

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
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

	quota := quotaBlock(st, sty.Dim(st.scanAge() > criticalAfter && !st.Snap.At.IsZero()), g)
	header := headerLines(st, lay, quota, sty, g)

	full := st.Height >= fullChromeHeight
	// The column-header row names the grid, so it appears only when the body
	// IS the grid. Over a help overlay or an empty state it would label
	// columns that are not on screen.
	showColumns := full && !st.Help && len(rows) > 0

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
		body = helpLines(st, lay, hasCtx, hasCost, sty)
	case len(rows) == 0:
		body = emptyLines(st, sty, g)
	default:
		body = rowLines(st, rows, lay, rowSty, g)
		isRows = true
	}

	hiddenBelow := 0
	if len(body) > bodyHeight {
		start := st.Scroll
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

// visibleSessions applies the vendor filter, the idle cutoff and the sort.
func visibleSessions(st State) []*model.Session {
	out := make([]*model.Session, 0, len(st.Snap.Sessions))
	for _, s := range st.Snap.Sessions {
		if !st.Filter.Accepts(s.Vendor) {
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
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex} {
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

// quotaBlock renders account-level quota, ONCE.
//
// rate_limits is a property of the account, not of the session: repeating it
// per row would assert per-session quota, which is false (§7.1). If no adapter
// can source it, the block is absent — not zeroed.
//
// v1 limitation, stated rather than hidden: the block shows the windows from
// the most recently active session that has any. With one quota-bearing vendor
// that is exact; a second one would need a per-vendor block, which is a change
// to this function and to the header layout, not to the schema.
func quotaBlock(st State, sty Styles, g Glyphs) string {
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
		return ""
	}

	parts := make([]string, 0, len(best.Quota))
	for i := range best.Quota {
		w := best.Quota[i]
		cell := sty.Muted.Render(w.Label) + " " + gauge(w.UsedPercent, quotaGauge, g, sty) + " "
		if w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100 {
			p := float64(*w.UsedPercent)
			cell += sty.Sev(p).Render(padLeft(theme.Percent(p), 5, g))
		} else {
			cell += sty.Absent().Render(padLeft(g.Absent, 5, g))
		}
		if w.ResetsAt != nil {
			if d := w.ResetsAt.Sub(st.Now); d > 0 {
				cell += " " + sty.Muted.Render(g.Reset+theme.Countdown(d))
			}
		}
		parts = append(parts, cell)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "   "+sty.Muted.Render(g.Sep)+"   ")
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
	for _, s := range rows {
		out = append(out, renderRow(st, s, lay, sty, g))
	}
	return out
}

func renderRow(st State, s *model.Session, lay Layout, sty Styles, g Glyphs) string {
	var b strings.Builder
	b.WriteString(" ")
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
	default:
		s := strings.ToUpper(string(v))
		if len(s) > 2 {
			s = s[:2]
		}
		return s
	}
}

// sessionLabel is the row's identity: the session's own name if the vendor has
// one, else the workspace basename, else the vendor session id.
//
// The parent directory is appended only when at least 14 cells remain free. It
// disambiguates same-named projects under different roots and stops the wide
// tier from opening a dead gulf between the name and the model, and it drops
// out automatically as the terminal narrows.
func sessionLabel(s *model.Session, width int, g Glyphs) string {
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
	label = truncate(label, width, g.Ellipsis)

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
	if st.Filter != FilterAll && len(st.Snap.Sessions) > 0 {
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

	var block []string
	for _, v := range st.Snap.Vendors {
		word := v.Status.String()
		styled := sty.Muted.Render(padRight(word, statusW, g))
		if v.Status == StatusUnreadable {
			// The one status where the operating system knows something the
			// user needs, so its own message travels with it.
			styled = sty.SevWarn.Render(padRight(word, statusW, g))
		}
		line := sty.Identity.Render(padRight(string(v.Vendor), rootW, g)) +
			"   " + styled +
			"   " + sty.Muted.Render(RedactHome(v.Root, st.Home))
		if v.Err != "" {
			line += sty.SevWarn.Render("  " + v.Err)
		}
		block = append(block, line)
	}

	// The heading centres on its own so that a long OS error in the vendor
	// table below cannot shove it sideways between frames.
	out := centerBlock([]string{sty.Text.Render(head)}, st.Width)
	out = append(out, "")
	return append(out, centerBlock(block, st.Width)...)
}

func helpLines(st State, lay Layout, hasCtx, hasCost bool, sty Styles) []string {
	pad := strings.Repeat(" ", 8)
	keys := [][2]string{
		{"q", "quit  (also esc, ctrl+c)"},
		{"v", "vendor filter: all -> claude -> codex"},
		{"s", "sort: activity -> context -> cost"},
		{"a", "show all (include sessions idle > 8h)"},
		{"r", "rescan now"},
		{"?", "close this help"},
	}
	out := []string{""}
	for _, k := range keys {
		out = append(out, pad+sty.Identity.Render(k[0])+"  "+sty.Text.Render(k[1]))
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
	var keys string
	if st.Help {
		keys = " " + sty.Muted.Render("? close")
	} else {
		hints := []string{"q quit", "v vendor", "s sort", "a all", "r refresh", "? keys"}
		switch tierFor(st.Width) {
		case TierNarrow:
			hints = []string{"q quit", "v vendor", "? keys"}
		case TierCompact:
			hints = []string{"q quit", "v vendor", "s sort", "? keys"}
		}
		keys = " " + sty.Muted.Render(strings.Join(hints, "   "))
	}

	var notices []string
	if hiddenBelow > 0 {
		notices = append(notices, sty.Muted.Render(fmt.Sprintf("+%d more", hiddenBelow)))
	}
	if st.Filter != FilterAll {
		// A monitor that silently hides rows is a liar: a non-default filter is
		// always stated.
		notices = append(notices, sty.Muted.Render("filter "+st.Filter.String()))
	}
	if st.Sort != SortActivity {
		notices = append(notices, sty.Muted.Render("sort "+st.Sort.String()))
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
			notices = append(notices, warn.Render(msg))
		}
	}
	if len(notices) == 0 {
		return keys
	}
	return joinEnds(keys, strings.Join(notices, "   "), st.Width)
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
	family := parts[1]
	switch family {
	case "opus", "sonnet", "haiku":
	default:
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
	return strings.ToUpper(family[:1]) + family[1:] + " " + strings.Join(rest, ".")
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
