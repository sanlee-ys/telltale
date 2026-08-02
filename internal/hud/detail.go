package hud

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// The detail pane (design.md §7.11).
//
// v1 carried Diagnostics, the Degraded field set and every Extra from adapter
// to renderer and displayed none of them. This pane is where the honesty
// machinery becomes product: the row says a cell is empty, and the pane says
// which of the two empties it is — "this vendor cannot source it" or "we tried
// and failed, here is what went wrong".
//
// It REPLACES the row area rather than floating over it, for the same reason
// the help overlay does: a panel covering the thing being monitored is a
// monitor you have to move to read.

const (
	// detailIndent aligns the label column with the SESSION column, so the
	// pane's first line is literally the selected row's identity zone and the
	// body hangs off it.
	detailIndent = 8
	// detailLabel is the label column. "diagnostics" is the longest label the
	// pane generates; vendor-supplied extras are truncated into it.
	detailLabel = 12
	// detailGap separates the label column from the value column.
	detailGap = 2
	// detailQuotaLabel is the per-window label cell inside a quota line.
	detailQuotaLabel = 3
	// detailQuotaPct is one column wider than the grid's, because this pane has
	// room for the estimate marker without stealing it from anything.
	detailQuotaPct = 6
)

// detailLines renders the pane for the selected session.
func detailLines(st State, rows []*model.Session, sty Styles, g Glyphs) []string {
	s := selectedSession(st, rows)
	if s == nil {
		// The selection went away between frames (the session ended, or a
		// filter removed it). Say that, rather than silently retargeting the
		// pane at whatever row now occupies the index.
		return []string{"", strings.Repeat(" ", detailIndent) +
			sty.Muted.Render("that session is no longer listed")}
	}

	caps := capsFor(st, s.Vendor)
	p := &pane{st: st, sty: sty, g: g}
	p.width = st.Width - detailIndent - detailLabel - detailGap - 1
	if p.width < 8 {
		p.width = 8
	}

	// Line one is the row's own left zone: dot, vendor, separator, label. The
	// pane opens where the row was.
	labelWidth := st.Width - 9
	p.out = append(p.out, " "+livenessDot(s, st, sty, g)+" "+
		sty.Identity.Render(vendorTag(s.Vendor))+" "+
		sty.Muted.Render(g.Sep)+" "+
		sty.Text.Render(sessionLabel(s, labelWidth, g)))

	// Identity.
	p.text("session", sty.Muted, s.ID)
	p.field(caps, s, model.FieldWorkspace, "workspace", func() {
		p.text("workspace", sty.Text, sanitize(*s.WorkspaceDir))
	})
	p.field(caps, s, model.FieldModel, "model", func() {
		p.text("model", sty.Identity, DisplayModel(s.Model))
	})

	// Measurement.
	p.field(caps, s, model.FieldContextPercent, "context", func() {
		p.raw("context", strings.TrimSpace(percentCell(s, sty, g)))
	})
	p.field(caps, s, model.FieldCost, "cost", func() {
		p.text("cost", sty.Text, theme.Cost(float64(*s.Cost)))
	})
	p.quota(caps, s)
	p.field(caps, s, model.FieldSubagents, "subagents", func() {
		p.text("subagents", sty.Text, subagentText(s))
	})

	// Time. This is the one line that never renders the absent marker: it
	// reports the LIVENESS CLASS, which the HUD can always produce, and the
	// age only as the evidence behind it. A session with no activity signal is
	// "unknown" — an em dash there would say "no value" where the truthful
	// statement is "no basis for a claim" (§4a.4).
	if caps.Capability(model.FieldLastActivity) != model.CapNone {
		p.text("activity", sty.Text, activityText(s, st, g))
	}

	// Extras are display-only by contract: no thresholds, no colours, no
	// sorting. This pane is the only place they appear (§4a.2).
	for _, e := range s.Extras {
		p.text(e.Label, sty.Text, sanitize(e.Value))
	}

	// The honesty block. Degraded and plain-absent render identically in the
	// grid on purpose; this is the one surface where the difference is stated.
	p.out = append(p.out, "")
	if names := fieldNames(s.Degraded); names != "" {
		p.text("degraded", sty.SevWarn, names)
	} else {
		p.text("degraded", sty.Absent(), g.Absent)
	}
	if len(s.Diagnostics) > 0 {
		for i, d := range s.Diagnostics {
			label := "diagnostics"
			if i > 0 {
				label = ""
			}
			p.text(label, sty.Muted, sanitize(d))
		}
	} else {
		p.text("diagnostics", sty.Absent(), g.Absent)
	}
	// "Can't know" versus "absent now" (§4a.1), named rather than left to be
	// inferred from a missing line.
	if names := fieldNames(unsourced(caps)); names != "" {
		p.text("not sourced", sty.Muted, names)
	}
	return p.out
}

// pane accumulates the label/value lines.
type pane struct {
	st    State
	sty   Styles
	g     Glyphs
	width int
	out   []string
}

// text places a PLAIN value in the value column, truncating before styling.
// Truncating after styling would measure escape codes as content, which is the
// bug that shears a grid in a coloured terminal while every golden passes.
func (p *pane) text(label string, sty lipgloss.Style, value string) {
	p.raw(label, sty.Render(truncate(value, p.width, p.g.Ellipsis)))
}

// raw places an already-styled, already-width-bounded value.
func (p *pane) raw(label, value string) {
	p.out = append(p.out,
		strings.Repeat(" ", detailIndent)+
			p.sty.Muted.Render(padRight(label, detailLabel, p.g))+
			strings.Repeat(" ", detailGap)+
			value)
}

// field renders one normalized field, honouring the two kinds of absence
// (§4a.1) as two different lines:
//
//   - can't know — the vendor declared CapNone. No line at all; the field is
//     named once on the "not sourced" line instead, exactly as the grid drops
//     a column no visible row can fill.
//   - absent now — declared but with no value this snapshot. The absent
//     marker, same as the grid's cell.
//
// render is only ever called when the field carries a value, which is what
// lets each closure dereference its pointer directly.
func (p *pane) field(caps model.Capabilities, s *model.Session, f model.Field, label string, render func()) {
	if caps.Capability(f) == model.CapNone {
		return
	}
	if !s.Has(f) {
		p.text(label, p.sty.Absent(), p.g.Absent)
		return
	}
	render()
}

// quota renders one line per window, labelled once. A window the vendor has
// but has no figure for renders the absent marker rather than 0%.
func (p *pane) quota(caps model.Capabilities, s *model.Session) {
	if caps.Capability(model.FieldQuota) == model.CapNone {
		return
	}
	if len(s.Quota) == 0 {
		p.text("quota", p.sty.Absent(), p.g.Absent)
		return
	}
	for i := range s.Quota {
		w := s.Quota[i]
		label := "quota"
		if i > 0 {
			label = ""
		}
		cell := p.sty.Muted.Render(padRight(w.Label, detailQuotaLabel, p.g)) + " " +
			gauge(w.UsedPercent, quotaGauge, p.g, p.sty) + " "
		if w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100 {
			pc := float64(*w.UsedPercent)
			cell += p.sty.Sev(pc).Render(padLeft(theme.Percent(pc), detailQuotaPct, p.g))
		} else {
			cell += p.sty.Absent().Render(padLeft(p.g.Absent, detailQuotaPct, p.g))
		}
		if w.ResetsAt != nil {
			if d := w.ResetsAt.Sub(p.st.Now); d > 0 {
				cell += "  " + p.sty.Muted.Render(p.g.Reset+theme.Countdown(d))
			}
		}
		p.raw(label, cell)
	}
}

// activityText states liveness and age together, because the classification is
// the HUD's and the age is the evidence for it. Unknown says so rather than
// borrowing "stale".
func activityText(s *model.Session, st State, g Glyphs) string {
	class := s.Liveness(st.Now, st.Thresholds)
	age, ok := s.Age(st.Now)
	if !ok {
		return class.String()
	}
	return class.String() + " " + g.Mid + " " + theme.Age(age) + " ago"
}

// subagentText renders the count with its estimate marker. Zero is a measured
// value and says so in words: the grid draws no chip for it, and a bare "0"
// here would be indistinguishable from a cell we could not read.
func subagentText(s *model.Session) string {
	mark := ""
	if s.Derived.Has(model.FieldSubagents) {
		mark = "~"
	}
	return mark + strconv.Itoa(*s.Subagents) + " recent"
}

// unsourced is every field this vendor declared it cannot source. Liveness is
// excluded: no adapter declares it, the HUD classifies it for every vendor
// from last_activity, and listing it would read as a gap rather than as the
// design (§4a.4).
func unsourced(caps model.Capabilities) model.FieldSet {
	var out model.FieldSet
	for _, f := range model.AllFields {
		if f == model.FieldLiveness {
			continue
		}
		if caps.Capability(f) == model.CapNone {
			out = out.With(f)
		}
	}
	return out
}

func fieldNames(fs model.FieldSet) string {
	if fs.Empty() {
		return ""
	}
	var names []string
	for _, f := range model.AllFields {
		if fs.Has(f) {
			names = append(names, f.String())
		}
	}
	return strings.Join(names, ", ")
}

// capsFor finds the vendor's declared capabilities in this snapshot. A vendor
// with no view (its Discover failed this scan) reports nothing, so the pane
// lists no normalized field rather than guessing at a capability table.
func capsFor(st State, v model.VendorID) model.Capabilities {
	for _, view := range st.Snap.Vendors {
		if view.Vendor == v {
			return view.Caps
		}
	}
	return model.Capabilities{}
}

// selectedSession resolves the cursor against the visible rows.
func selectedSession(st State, rows []*model.Session) *model.Session {
	if st.Cursor < 0 || st.Cursor >= len(rows) {
		return nil
	}
	return rows[st.Cursor]
}
