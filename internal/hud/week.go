// The week page (design.md §7.19): one line per vendor, the slow windows only.
//
// The fleet usage view (§7.17) answers "what can telltale say about each
// vendor's account" and spends a block per vendor to say it. This page answers
// a narrower question the owner actually asked — "how much of the week is
// left, per lane, for scoping work" — and a scoping glance does not want the
// census, the spend lines or the five-hour windows that reset before the work
// starts. It is a LENS over §7.17's data, never a second source: every reading
// here comes from the same quotaVendors/usageBlocks assembly the u page
// renders, so the two surfaces cannot disagree about an account.
//
// # What earns a row, and what is kept off
//
//   - A vendor with a quota reading shows its weekly-class windows (see
//     weeklyWindows for what that means and why it is honest).
//   - A vendor with no reading shows §7.17's own absence sentence — the same
//     three-kinds-of-nothing vocabulary, not an em dash. A dash would say "no
//     reading now"; cursor and grok are structurally unreadable, and the
//     sentence is what keeps those two states apart (§4a.1).
//   - SPEND does not appear here at all, and not because it is unimportant. A
//     spend total's accumulation window is "sessions on disk, this scan"
//     (§7.16) — not a week, not any calendar span. Rendering it under a page
//     titled "this week" would claim a window the number does not have, which
//     is the exact blur §7.17's two-vocabularies rule exists to prevent. The u
//     page renders spend correctly, one key away.
//
// # Ordering
//
// Blocks keep usageBlocks's fleet order. Sorting by remaining headroom was
// considered and rejected: readings, absences and two-window vendors do not
// order on one axis, and a page that reshuffles when a percentage moves spends
// more than §7.1 rule 4's churn budget to encode nothing a reader cannot see
// in the percentages themselves.
//
// # The relayed age rides every row
//
// A relayed reading is exactly as old as the last statusline render, and §7.15
// rules that shedding the age re-presents a stale number as fresh. The u page
// carries the age on the vendor's heading; this page has no headings, so the
// age rides each row as a suffix, and past quotaAgeWarn it escalates in the
// header's own grammar — the word (quotaAgeWord), the glyph, then the hue as
// the second signal (§7.1 rule 2). The REASON sentence does not travel here;
// §7.17's page carries the argument, this one carries the alarm.

package hud

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// weekVendorCol is the vendor-name column. The longest vendor id is six cells
// ("claude", "gemini", "cursor"); the seventh is the clear cell before the
// window label, so a continuation row's blank lead stays visibly a
// continuation rather than a ragged indent.
const weekVendorCol = 7

// weekHeadingNotes is the page's legend, in dress levels like §7.17's. The
// long level states the selection rule in the vendor's own terms; nothing
// here claims a duration, because each row's label carries its own (see
// weeklyWindows).
var weekHeadingNotes = []string{
	"each vendor's longest window, and every window it names weekly",
	"the slow windows",
	"",
}

// weeklyWindows selects the windows the week page speaks for: every window
// the vendor itself names weekly, plus the vendor's longest.
//
// Neither leg infers a duration, and that constraint is the whole design.
// quotaAgeWarn's ruling stands one file over: a length parsed out of a
// display label ("5h", "7d") is the class of guess §4a.1 rejects. So:
//
//   - the "-weekly" suffix is vendor vocabulary, read verbatim. agy names its
//     buckets ("3p-weekly", "gemini-weekly" observed, §3.8), quotacache
//     carries the names as ids unchanged (its convert.go says why), and
//     reading the vendor's own suffix is reading, not translating.
//   - the LAST window rides model.QuotaWindow's ordering contract — "display
//     order, shortest first" — so it is the vendor's longest pool by
//     structure rather than by arithmetic. Claude's slice ends on seven_day
//     and Codex's on secondary; no id is parsed for a length, and the row
//     renders the vendor's own label beside the reading, so the page never
//     states a duration the vendor did not.
//
// The union is deduped by id, keeping slice order. One edge is deliberate: a
// vendor whose only surviving window is short — Claude relayed after
// quotacache dropped an expired seven_day — shows that window under its own
// label. That is the honest render: it is the longest reading telltale holds,
// and the label beside it says how long it is.
func weeklyWindows(windows []model.QuotaWindow) []model.QuotaWindow {
	var out []model.QuotaWindow
	seen := map[string]bool{}
	for _, w := range windows {
		if strings.HasSuffix(w.ID, "-weekly") && !seen[w.ID] {
			seen[w.ID] = true
			out = append(out, w)
		}
	}
	if n := len(windows); n > 0 {
		if last := windows[n-1]; !seen[last.ID] {
			out = append(out, last)
		}
	}
	return out
}

// weekLines renders the page. No air distribution and no room parameter: this
// body is a tight table of one or two rows per vendor, and §7.17's void fix
// (usageAir) exists for a page of multi-line blocks, which this is not.
func weekLines(st State, sty Styles, g Glyphs) []string {
	cells := usageGaugeFor(st.Width)
	out := []string{weekHeading(st, sty, g), ""}

	blocks := usageBlocks(st)
	if len(blocks) == 0 {
		// §7.7's rule, same words as the u page: an empty state says which
		// emptiness it is, and this one is a measurement over every vendor.
		return append(out, strings.Repeat(" ", usageIndent)+
			sty.Muted.Render("no vendor on this machine has reported a quota reading or a token count"))
	}
	for _, b := range blocks {
		out = append(out, weekBlockLines(st, b, cells, sty, g)...)
	}
	return out
}

// weekHeading mirrors usageHeading: the title, the legend, and the heavy rule
// taking what is left. One page family, one heading grammar.
func weekHeading(st State, sty Styles, g Glyphs) string {
	head := usageFit(" "+sty.Text.Render("this week"), weekHeadingNotes, st.Width, sty.Muted, g)
	room := st.Width - 1 - lipgloss.Width(head) - 2
	if room < usageRuleMin {
		return head
	}
	return head + "  " + sty.Rule().Render(strings.Repeat(g.RuleHeavy, room))
}

// weekBlockLines renders one vendor: its weekly-class windows, or the §7.17
// absence sentence when it has no reading. The vendor name leads the first
// row only — a second window (agy's two weekly pools) continues under a blank
// lead, because the name repeated would read as a second vendor.
func weekBlockLines(st State, b usageBlock, cells int, sty Styles, g Glyphs) []string {
	// The vendor name spends the per-vendor hue exactly as the u page's
	// headings do (§7.17): blocks stack in one column here too, so position
	// answers nothing about which vendor a row belongs to. Padded before
	// styling — padRight walks runes and must never see an escape sequence.
	head := " " + sty.VendorIdentity(b.vendor).Render(padRight(string(b.vendor), weekVendorCol, g))

	if b.quota == nil {
		return []string{usageFit(head, usageQuotaAbsence(b.vendor, g), st.Width, sty.Muted, g)}
	}

	suffix := weekAgeSuffix(*b.quota, sty, g)
	cont := strings.Repeat(" ", 1+weekVendorCol)
	var out []string
	for i, w := range weeklyWindows(b.quota.windows) {
		lead := cont
		if i == 0 {
			lead = head
		}
		out = append(out, lead+weekWindowCell(st, w, cells, suffix, sty, g))
	}
	return out
}

// weekWindowCell is one reading: label, gauge, percentage, countdown, and the
// relay-age suffix. The vocabulary is §7.17's quota vocabulary unchanged —
// this page renders no reading the u page would render differently.
//
// A window with no figure renders the absent marker and NO bar, never 0%
// (§7.1 principle 1) — the same distinction usageQuotaLines keeps, kept here
// for the same reason.
func weekWindowCell(st State, w model.QuotaWindow, cells int, suffix string, sty Styles, g Glyphs) string {
	var v strings.Builder
	v.WriteString(sty.Muted.Render(padRight(w.Label, usageLabel, g)))
	v.WriteString(strings.Repeat(" ", usageGap))
	if cells > 0 {
		v.WriteString(gauge(w.UsedPercent, cells, g, sty))
		v.WriteString(" ")
	}
	if w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100 {
		p := float64(*w.UsedPercent)
		v.WriteString(sty.Sev(p).Render(padLeft(theme.Percent(p), usagePct, g)))
	} else {
		v.WriteString(sty.Absent().Render(padLeft(g.Absent, usagePct, g)))
	}
	if w.ResetsAt != nil {
		if d := w.ResetsAt.Sub(st.Now); d > 0 {
			// The space between the glyph and the digits is the header's own
			// dogfood finding: ↻ renders at ambiguous width and reads as one
			// garbled token when glued on.
			v.WriteString("  " + sty.Muted.Render(g.Reset+" "+theme.Countdown(d)))
		}
	}
	if suffix != "" {
		v.WriteString("  " + suffix)
	}
	return v.String()
}

// weekAgeSuffix is the relay age, rendered whole in one style so no escape
// sequence ever sits mid-string (CLAUDE.md's ANSI trap; usageFit's precedent).
//
// Under quotaAgeShown the suffix is empty — a seconds-old relay reading needs
// no age any more than the header gives it one. Past quotaAgeWarn the WORD
// carries the escalation before the hue does: "stale" is quotaAgeWord, the
// header's own verdict vocabulary, and SevWarn is the second signal.
func weekAgeSuffix(b quotaVendorBlock, sty Styles, g Glyphs) string {
	if !b.relayed || b.age < quotaAgeShown {
		return ""
	}
	if b.age >= quotaAgeWarn {
		return sty.SevWarn.Render(g.Mid + " " + g.Warn + " " + quotaAgeWord + " " + theme.Age(b.age) + " ago")
	}
	return sty.Muted.Render(g.Mid + " " + theme.Age(b.age) + " ago")
}
