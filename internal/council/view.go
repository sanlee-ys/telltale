package council

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Render draws one frame.
//
// Pure over State: no clock, no filesystem, no environment. Tests construct a
// State by hand and diff the result against internal/council/testdata/golden —
// the same contract internal/hud renders under (design.md §7.9), and the reason
// council's layout can be verified without a terminal or a vendor process.
func Render(st State, sty Styles, g Glyphs) string {
	if st.Width < MinWidth || st.Height < MinHeight {
		return floorMessage(st, sty)
	}

	lay := resolveLayout(st.Width, st.Height, len(st.Columns))

	// Every assembled line goes through fit at its final width. The rule that no
	// rendered line may exceed the terminal is enforced here, once, rather than
	// trusted to the gap arithmetic in each builder below.
	var b strings.Builder
	b.WriteString(fit(header(st, lay, sty, g), st.Width))
	b.WriteString("\n")
	b.WriteString(rule(st.Width, sty, g))
	b.WriteString("\n")

	if st.Help {
		b.WriteString(helpBody(st, lay, sty, g))
	} else if lay.Tier == TierTabs {
		b.WriteString(tabBar(st, lay, sty, g))
		b.WriteString("\n")
		b.WriteString(tabBody(st, lay, sty, g))
	} else {
		b.WriteString(columnsBody(st, lay, sty, g))
	}

	b.WriteString("\n")
	b.WriteString(rule(st.Width, sty, g))
	b.WriteString("\n")
	b.WriteString(fit(promptLine(st, lay, sty, g), st.Width))
	b.WriteString("\n")
	b.WriteString(fit(modeLine(st, lay, sty, g), st.Width))
	return b.String()
}

// floorMessage is what a terminal too small to draw the room gets. It names the
// number needed rather than just refusing, so the user knows how far to drag.
func floorMessage(st State, sty Styles) string {
	want := "council needs " + strconv.Itoa(MinWidth) + "x" + strconv.Itoa(MinHeight)
	got := " (have " + strconv.Itoa(st.Width) + "x" + strconv.Itoa(st.Height) + ")"
	return sty.Muted.Render(want + got)
}

func rule(w int, sty Styles, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	return sty.Rule().Render(strings.Repeat(g.Rule, w))
}

// header names the workspace and the round.
//
// The workspace is on screen at all times and is not decoration: every turn is
// dispatched with it as the working directory, so which directory this is
// changes what the agents can see. A dispatch room that hid its own cwd would
// be the same class of omission as a gauge that hid its units.
func header(st State, lay Layout, sty Styles, g Glyphs) string {
	left := sty.Identity.Render("council")

	round := "no turn yet"
	if st.Turn > 0 {
		round = "turn " + strconv.Itoa(st.Turn)
	}
	seated := strconv.Itoa(st.Seated()) + "/" + strconv.Itoa(len(st.Columns)) + " seated"
	right := sty.Muted.Render(round + "  " + g.Sep + "  " + seated)

	// The path takes whatever is left, elided from the left because the
	// uninformative part of a path is its prefix.
	used := lipgloss.Width(left) + lipgloss.Width(right) + 4
	pathw := lay.Width - used
	mid := ""
	if pathw > 3 {
		mid = sty.Muted.Render(elideLeft(displayPath(st), pathw, g.Ellipsis))
	}

	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + left + " " + mid + strings.Repeat(" ", gap) + right + " "
}

// displayPath abbreviates the home prefix. Display only — the dispatched
// working directory is always the resolved absolute path.
func displayPath(st State) string {
	p := st.Workspace
	if st.Home != "" && strings.HasPrefix(p, st.Home) {
		return "~" + p[len(st.Home):]
	}
	return p
}

// columnsBody draws the seats side by side.
func columnsBody(st State, lay Layout, sty Styles, g Glyphs) string {
	cells := make([][]string, len(st.Columns))
	widths := make([]int, len(st.Columns))
	for i, c := range st.Columns {
		w := lay.ColWidth + lay.extraFor(i)
		widths[i] = w
		cells[i] = columnCell(st, c, i == st.Focus, w, lay.Body, sty, g)
	}

	sep := " " + sty.Rule().Render(g.Sep) + " "
	var b strings.Builder
	for row := 0; row < lay.Body; row++ {
		b.WriteString(" ")
		for i := range st.Columns {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(cells[i][row])
		}
		b.WriteString(" ")
		if row < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// columnCell renders one column to exactly h lines of exactly w cells.
//
// Returning a fixed rectangle is what keeps the side-by-side join honest: a
// short column pads rather than collapsing, so a vendor that has said nothing
// yet occupies its seat instead of letting its neighbours slide left.
func columnCell(st State, c Column, focused bool, w, h int, sty Styles, g Glyphs) []string {
	lines := make([]string, 0, h)

	lines = append(lines, fit(columnHeader(st, c, focused, w, sty, g), w))
	if len(lines) < h {
		lines = append(lines, sty.Muted.Render(strings.Repeat(g.Rule, w)))
	}

	for _, l := range columnText(c, w, g) {
		if len(lines) >= h {
			break
		}
		lines = append(lines, padRight(l, w, g))
	}

	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// columnHeader is the vendor name plus the two claims this product refuses to
// leave implicit: what its sandbox actually is, and how finely it reports.
func columnHeader(st State, c Column, focused bool, w int, sty Styles, g Glyphs) string {
	name := c.Label
	if focused {
		name = g.Focus + name
	} else {
		name = " " + name
	}

	status := c.Phase.String()
	if c.Avail != AvailInstalled {
		status = "unavailable"
	} else if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		if len(g.Spinner) > 0 {
			status = g.Spinner[st.Spinner%len(g.Spinner)] + " " + status
		}
	}

	left := sty.Identity.Render(padRight(name, maxInt(1, w-lipgloss.Width(status)-1), g))
	right := sty.ForPhase(c.Phase).Render(status)
	if c.Avail != AvailInstalled {
		right = sty.SevWarn.Render(status)
	}
	head := left + " " + right

	// The badges get their own line when there is room; below that they are the
	// first thing dropped, because a claim nobody can read is not a claim.
	if w < 20 {
		return truncate(head, w, g.Ellipsis)
	}
	return head
}

// columnText is a column's body: its output, or the card explaining why there
// is none.
func columnText(c Column, w int, g Glyphs) []string {
	if c.Avail != AvailInstalled {
		return unavailableCard(c, w, g)
	}

	var out []string
	if b := badgeLine(c); b != "" {
		out = append(out, wrap(b, w)...)
		out = append(out, "")
	}

	switch {
	case c.Phase == PhaseIdle && c.Body == "":
		out = append(out, wrap("no turn dispatched yet.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "":
		// The honest version of an empty streaming column. This vendor is
		// working; it just does not report anything until it is done, and
		// pretending otherwise would be a fabricated progress signal.
		out = append(out, wrap("working. this vendor reports no incremental output, so nothing appears until the turn finishes.", w)...)
	default:
		out = append(out, wrap(c.Body, w)...)
	}

	if c.Note != "" {
		out = append(out, "")
		out = append(out, wrap(g.Warn+" "+c.Note, w)...)
	}
	return out
}

// badgeLine is the sandbox claim, the streaming granularity, and the cost.
//
// Cost renders only when the vendor REPORTED one. A turn that reported zero
// shows $0.0000; a turn that reported nothing shows no cost cell at all. Those
// are different facts, and deriving a figure from token counts is on this
// repo's deliberately-rejected list (design.md §8) — council does not get to
// invent dollars either.
func badgeLine(c Column) string {
	parts := []string{}
	if b := c.Sandbox.Badge(); b != "" {
		parts = append(parts, b)
	}
	if s := c.Gran.String(); s != "" {
		parts = append(parts, s)
	}
	if c.CostUSD != nil {
		parts = append(parts, "$"+strconv.FormatFloat(*c.CostUSD, 'f', 4, 64))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ")
}

// unavailableCard says which failure this is and what would fix it. Absence and
// unusability are different facts and get different words — the HUD's rule that
// a dropped column and an em dash must not read alike (§4a.1), applied here.
func unavailableCard(c Column, w int, g Glyphs) []string {
	out := wrap(g.Warn+" "+c.Label+" is not seated", w)
	if c.Note != "" {
		out = append(out, "")
		out = append(out, wrap(c.Note, w)...)
	}
	out = append(out, "")
	out = append(out, wrap("the other columns dispatch normally.", w)...)
	return out
}

// tabBar is the narrow-terminal alternative to side-by-side columns.
func tabBar(st State, lay Layout, sty Styles, g Glyphs) string {
	var parts []string
	for i, c := range st.Columns {
		label := c.Label
		if c.Avail != AvailInstalled {
			label += " " + g.Warn
		}
		if i == st.Focus {
			parts = append(parts, sty.Identity.Render(g.Focus+label))
		} else {
			parts = append(parts, sty.Muted.Render(" "+label))
		}
	}
	// fit, not padRight: parts are already styled per tab.
	return " " + fit(strings.Join(parts, "  "), lay.Width-2) + " "
}

func tabBody(st State, lay Layout, sty Styles, g Glyphs) string {
	if len(st.Columns) == 0 {
		return strings.Repeat("\n", maxInt(0, lay.Body-1))
	}
	idx := st.Focus
	if idx < 0 || idx >= len(st.Columns) {
		idx = 0
	}
	// focused=false: the tab bar directly above already carries the marker, and
	// a second one on the only visible column is noise.
	cell := columnCell(st, st.Columns[idx], false, lay.ColWidth, lay.Body, sty, g)
	var b strings.Builder
	for i, l := range cell {
		b.WriteString(" " + l + " ")
		if i < len(cell)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// promptLine is the draft, with the caret at the insertion point.
//
// The caret is static, never blinking: the HUD budgets exactly one moving cell
// on screen and council keeps the same budget — here it is spent on the
// spinner of a column that is actually working.
func promptLine(st State, lay Layout, sty Styles, g Glyphs) string {
	prefix := g.Prompt + " "
	w := lay.Width - 2 - lipgloss.Width(prefix)
	if w < 1 {
		w = 1
	}

	text := st.Draft
	if st.Mode == ModeComposing {
		text += g.Caret
	}
	// Elide from the LEFT while typing: the tail is where the cursor is, and a
	// prompt that hides the characters just typed would be unusable.
	if lipgloss.Width(text) > w {
		text = elideLeft(text, w, g.Ellipsis)
	}

	// Pad the PLAIN text, then style it once — never the other way round.
	body := sty.Text.Render(padRight(text, w, g))
	if st.Draft == "" && st.Mode == ModeComposing {
		body = sty.Muted.Render(padRight("type a brief — @claude, @codex or @agy to address one"+g.Caret, w, g))
	}
	return " " + sty.Muted.Render(prefix) + body + " "
}

// modeLine announces which mode the room is in and what the keys mean in it.
//
// Always visible, never inferred: a mode that changes what an unmodified key
// means without saying so is the failure design.md §7.8 names by name, and
// council has a mode where `q` is the letter q.
func modeLine(st State, lay Layout, sty Styles, g Glyphs) string {
	var left, right string
	switch st.Mode {
	case ModeComposing:
		left = "COMPOSE"
		// The routing is stated before the keybindings because it is the one
		// thing on this line that changes what enter DOES. An @typo has to read
		// as "this is going to everyone" while there is still time to fix it;
		// discovering it afterwards means a wasted turn against three quotas.
		right = "→ " + routeLabel(st) + "  " + g.Sep + "  enter dispatch  " + g.Sep + "  esc view"
	default:
		left = "VIEW"
		if st.Busy() {
			right = "tab focus  " + g.Sep + "  ctrl+c cancel turn  " + g.Sep + "  ? help"
		} else {
			right = "tab focus  " + g.Sep + "  i compose  " + g.Sep + "  ? help  " + g.Sep + "  q quit"
		}
	}
	if st.Notice != "" {
		right = g.Warn + " " + st.Notice
	}

	l := sty.Identity.Render(left)
	r := sty.Muted.Render(right)
	gap := lay.Width - lipgloss.Width(l) - lipgloss.Width(r) - 2
	if gap < 1 {
		gap = 1
		r = truncate(right, maxInt(1, lay.Width-lipgloss.Width(l)-3), g.Ellipsis)
		r = sty.Muted.Render(r)
	}
	return " " + l + strings.Repeat(" ", gap) + r + " "
}

// routeLabel names who the current draft is addressed to.
//
// "everyone" rather than a blank or an em dash: this cell always has an answer,
// because a brief with no mention is not an absent routing, it is a routing to
// the whole room. The em dash is reserved for facts the product does not have.
func routeLabel(st State) string {
	if len(st.Route) == 0 {
		return "everyone"
	}
	return strings.Join(st.Route.labels(), ", ")
}

// helpBody replaces the column area, rather than floating over it, for the same
// reason the HUD's overlay does: a panel that covers live output hides the
// thing the user is watching.
func helpBody(st State, lay Layout, sty Styles, g Glyphs) string {
	lines := []string{
		sty.Identity.Render("council") + sty.Muted.Render(" — one brief, several agents, side by side"),
		"",
		"  i / enter    compose a brief",
		"  enter        dispatch — to everyone, or to whoever is @mentioned",
		"  @claude      address one vendor: @claude, @codex, @agy, @all",
		"               leading mentions only, so \"ask @claude\" is just prose",
		"  esc          leave compose (the draft is kept)",
		"  tab          move focus between columns",
		"  ctrl+c       cancel the turn in flight, or quit when idle",
		"  q            quit (in view mode only — in compose it is the letter q)",
		"  ?            this help",
		"",
		sty.Muted.Render("  council dispatches to vendor CLIs. It is the one telltale mode that"),
		sty.Muted.Render("  does, and each column states its own read-only posture rather than"),
		sty.Muted.Render("  the room making one claim on behalf of all three."),
	}

	var b strings.Builder
	for i := 0; i < lay.Body; i++ {
		if i < len(lines) {
			// fit, not padRight: some help lines are pre-styled.
			b.WriteString(" " + fit(lines[i], lay.Width-2) + " ")
		} else {
			b.WriteString(strings.Repeat(" ", lay.Width))
		}
		if i < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
