package council

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/grok"
	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The seat's model: what the room ASKED for, and what the vendor's own record
// says RAN (added 2026-09-30).
//
// The two halves are two different sources, and the room keeps them apart.
//
//   - The request is what council put on the vendor's argv. `--model` sets it
//     per seat, and vendors.WithModel puts it where that CLI parses it. It is
//     a fact about council, not about the vendor.
//   - The resolved model is what the vendor's own on-disk session record names
//     for the session this seat holds. It is read with the HUD's own adapters,
//     the same readers the HUD's model column uses, so the room and the HUD
//     cannot give two answers for one session. Where no record can be found,
//     or the record names no model, the room prints the word `unknown`.
//
// Two sources are refused, by name. The seat's own answer to "which model are
// you" is generated text, not introspection, so no reply text is read here. And
// the vendor's stream init frame is not read either: claude.go leaves its
// `model` field unread on purpose, and this feature does not change that.
//
// A request with no read is the trap this feature exists to close. A room that
// said only `asked grok-4.5` would read the dial it was set to. An auto route
// that overrode the request would never show, and a seat that is Claude in a
// different harness would look independent (harness is not model
// independence).

// SeatModel is one seat's model, as the room can state it.
//
// A pointer on Column, and nil is the ordinary room: no request and no read.
// Nil draws nothing, which keeps every golden built before this feature as it
// was.
type SeatModel struct {
	// Requested is the name council passed on argv. Empty means the room asked
	// for nothing and the vendor chose its own default. That is a different
	// fact from a request, and the cell says so by drawing no `asked` word.
	Requested string
	// Read reports that a read of the vendor's session record returned for the
	// session this seat holds now. False means no read has returned yet, which
	// is absence, not unknown.
	Read bool
	// Resolved is the model the vendor's record names. Empty with Read true is
	// the measured unknown: the room looked and the record did not say.
	Resolved string
}

// Mismatch reports that the vendor's record names a different model from the
// one the room asked for.
//
// The comparison is textual and ignores letter case, and nothing more. An
// alias the vendor expands (`opus` to a full model id) is a mismatch by this
// rule, and the cell shows both names so the reader can judge. The room does
// not keep an alias table: a table of which name means which model would be
// the room's guess presented as the vendor's statement (§4a.1). Pass the full
// model id to compare exactly.
func (m *SeatModel) Mismatch() bool {
	return m != nil && m.Requested != "" && m.Read && m.Resolved != "" &&
		!strings.EqualFold(m.Requested, m.Resolved)
}

// seatModelUnknown is the word for a read that found no model. A word and not
// a glyph, so --ascii and NO_COLOR keep it whole, and not the absent dash,
// because a dash means "nothing to show" and this means "the room looked".
const seatModelUnknown = "unknown"

// modelForm is one rung of the model cell's shed ladder. reading is the
// vendor's name, drawn in the measured ink; the rest is chrome.
type modelForm struct {
	lead    string // the mismatch mark and its space, or empty
	asked   string // "asked <name>  ", or empty
	ran     string // "ran ", or empty
	reading string // the resolved name, or `unknown`
	tail    string // " as asked", or empty
}

func (f modelForm) plain() string {
	return f.lead + f.asked + f.ran + f.reading + f.tail
}

func (f modelForm) render(sty Styles, mismatch bool) string {
	chrome := sty.Muted
	if mismatch {
		// The whole form at severity, not only the mark. A string with escapes
		// in its middle is one the narrow-width paths may cut through (the
		// ANSI trap in view.go).
		return sty.SevWarn.Render(f.plain())
	}
	out := chrome.Render(f.lead + f.asked + f.ran)
	if f.reading != "" {
		out += sty.Measured.Render(f.reading)
	}
	return out + chrome.Render(f.tail)
}

// modelForms is the model cell's ladder, most dressed first.
//
// What sheds is the half a reader can do without, and never the mark:
//
//   - a match sheds `as asked`, then keeps `ran <name>`.
//   - a mismatch sheds `asked <name>`, then keeps `⚠ ran <name>`. The mark
//     stays on the last rung, so a narrow column still says the two differ.
//   - an unknown sheds `asked <name>`, then keeps `ran unknown`.
//   - a request with no read has one form, `asked <name>`.
func modelForms(m *SeatModel, g Glyphs) []modelForm {
	if m == nil {
		return nil
	}
	switch {
	case !m.Read && m.Requested == "":
		return nil
	case !m.Read:
		return []modelForm{{asked: "asked " + m.Requested}}
	}
	reading := m.Resolved
	if reading == "" {
		reading = seatModelUnknown
	}
	bare := modelForm{ran: "ran ", reading: reading}
	switch {
	case m.Requested == "":
		return []modelForm{bare}
	case m.Mismatch():
		bare.lead = g.Warn + " "
		dressed := bare
		dressed.asked = "asked " + m.Requested + "  "
		return []modelForm{dressed, bare}
	case m.Resolved == "":
		dressed := bare
		dressed.asked = "asked " + m.Requested + "  "
		return []modelForm{dressed, bare}
	default:
		dressed := bare
		dressed.tail = " as asked"
		return []modelForm{dressed, bare}
	}
}

// seatModelCell picks the widest rung that fits `avail` cells, or nothing. It
// returns the plain text beside the styled string, because badgeRow's width
// arithmetic runs over the plain copy.
func seatModelCell(m *SeatModel, avail int, sty Styles, g Glyphs) (styled, plain string) {
	for _, f := range modelForms(m, g) {
		if p := f.plain(); lipgloss.Width(p) <= avail {
			return f.render(sty, m.Mismatch()), p
		}
	}
	return "", ""
}

// ParseModels turns a --model value into one request per seat:
// `claude=opus,codex=gpt-5.6-sol`.
//
// The seat names are the @mention vocabulary, as ParseSeats uses. `all` is
// refused, because one model name does not mean one thing across vendors.
//
// A seat with no measured model flag is REFUSED here, by name, before the room
// opens (vendors.ErrModelNotSupported). The other choice was to take the
// request and not pass it, and that would leave the operator believing a seat
// runs a model nothing asked for.
func ParseModels(s string) (map[model.VendorID]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	aliases := mentionAliases()
	out := map[model.VendorID]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		seat, name, ok := strings.Cut(part, "=")
		seat = strings.ToLower(strings.TrimSpace(seat))
		name = strings.TrimSpace(name)
		if !ok || seat == "" {
			return nil, fmt.Errorf("--model %s: write seat=model, for example codex=gpt-5.6-sol", part)
		}
		if allAliases[seat] {
			return nil, fmt.Errorf("--model %s: a model is asked for per seat, and one name does not mean one model across vendors", part)
		}
		v, known := aliases[seat]
		if !known {
			return nil, fmt.Errorf("--model %s: unknown seat %s (want %s)", part, seat, strings.Join(SeatNames(), ", "))
		}
		if _, dup := out[v]; dup {
			return nil, fmt.Errorf("--model %s: %s is named twice", part, v)
		}
		if err := vendors.ValidModelName(name); err != nil {
			return nil, fmt.Errorf("--model %s: %w", part, err)
		}
		if _, err := vendors.ModelFlagFor(v); err != nil {
			return nil, fmt.Errorf("--model %s: %w, so the request is refused rather than dropped", part, err)
		}
		out[v] = name
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// withSeatModel puts this room's request for the spec's seat on its argv, or
// returns the spec unchanged when the room asked for nothing.
//
// Every spawn path calls it: the batch turn (specFor), the live seat
// (spawnSeat), and both racer shapes. A path that skipped it would run the
// vendor default under a column that says `asked <name>`.
func (m *Model) withSeatModel(spec runner.Spec) (runner.Spec, error) {
	return vendors.WithModel(spec, m.opts.Models[spec.Vendor])
}

// seatModelMsg is one finished read of every seat's session record.
type seatModelMsg struct {
	// readings holds one entry per seat that held a session id when the read
	// started, keyed by vendor.
	readings map[model.VendorID]seatModelReading
}

// seatModelReading is one seat's read: the session id it looked up, and the
// resolved model, or empty for unknown.
//
// The id travels with the name so a read can be matched to the session it was
// made for. Two reads can be in flight at once (two dispatches end close
// together), and a slow read of an old session must not land on a seat that
// now holds a new one: that would print a real name from the wrong
// conversation.
type seatModelReading struct {
	session string
	model   string
}

// seatModelSources maps a seat to the HUD adapter that reads its vendor's
// session store. A var so a test can point each adapter at the adapter's own
// fixture tree; production never reassigns it.
//
// The key that joins the two is the vendor's session id. The room receives it
// on the vendor's own stream (m.sessions), and each adapter keys its sessions
// by the id in the store's file or directory name:
//
//   - claude: projects/<slug>/<session_id>.jsonl, the newest non-synthetic
//     assistant `message.model` (internal/adapter/claudecode).
//   - codex: sessions/<y>/<m>/<d>/rollout-<ts>-<thread_id>.jsonl, the last
//     `turn_context.payload.model` (internal/adapter/codex). The thread id is
//     the key `codex exec resume` looks the rollout up by (codex.go).
//   - agy: conversations/<conversation_id>.db and its transcript, the
//     generation metadata's model (internal/adapter/antigravity).
//   - grok: sessions/<workspace>/<session_id>/summary.json,
//     `current_model_id` (internal/adapter/grok).
//   - cursor: the Cursor store's `modelConfig.modelName`, verbatim, which can
//     be the unresolved alias `default` (internal/adapter/cursor).
//
// Which of those joins hold on a live seat is recorded in PARITY.md, dated.
// A join that does not hold finds no record, and the cell then says
// `unknown`. It never falls back to a nearby session, because a nearby
// session is a different conversation.
var seatModelSources = map[model.VendorID]func() model.Adapter{
	model.VendorClaude:      func() model.Adapter { return claudecode.New() },
	model.VendorCodex:       func() model.Adapter { return codex.New() },
	model.VendorAntigravity: func() model.Adapter { return antigravity.New() },
	model.VendorGrok:        func() model.Adapter { return grok.New() },
	model.VendorCursor:      func() model.Adapter { return cursor.New() },
}

// seatModelReadTimeout bounds one read of every seat. A read that takes longer
// is abandoned and those seats say `unknown`, which is what the room can
// state about a record it could not reach.
const seatModelReadTimeout = 10 * time.Second

// readSeatModel reads the model one vendor's record names for one session id.
// Empty is unknown, for every reason: no adapter, no store, no session with
// that id, a failed read, or a record with no model in it.
func readSeatModel(ctx context.Context, v model.VendorID, sessionID string) string {
	mk, ok := seatModelSources[v]
	if !ok || sessionID == "" {
		return ""
	}
	a := mk()
	refs, err := a.Discover(ctx)
	if err != nil {
		return ""
	}
	for _, r := range refs {
		if r.ID != sessionID {
			continue
		}
		s, err := a.Read(ctx, r)
		if err != nil || s == nil || s.Model == nil {
			return ""
		}
		// The id before the display name. The request is an id, and a
		// comparison of an id with a display name would be a mismatch on
		// every seat whose vendor writes both.
		if s.Model.ID != "" {
			return s.Model.ID
		}
		return s.Model.DisplayName
	}
	return ""
}

// readSeatModelsCmd reads every seat's record off the update loop.
//
// A Cmd for readQuotaCmd's reason: Render must stay pure over State, and this
// read touches the filesystem. It runs when a dispatch ends, which is when a
// seat can first hold a session id and when a vendor can have moved to a new
// model. The ids are copied here, on the update loop, so the goroutine never
// reads m.sessions.
func (m *Model) readSeatModelsCmd() tea.Cmd {
	ids := make(map[model.VendorID]string, len(m.sessions))
	for v, id := range m.sessions {
		if id != "" {
			ids[v] = id
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), seatModelReadTimeout)
		defer cancel()
		// Sorted so the reads run in one order every time. Nothing depends on
		// it today; a trace of this read would.
		order := make([]model.VendorID, 0, len(ids))
		for v := range ids {
			order = append(order, v)
		}
		sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
		out := make(map[model.VendorID]seatModelReading, len(ids))
		for _, v := range order {
			out[v] = seatModelReading{session: ids[v], model: readSeatModel(ctx, v, ids[v])}
		}
		return seatModelMsg{readings: out}
	}
}

// applySeatModels lands one read on the columns.
//
// Each seat is decided against the session it holds NOW, not the one the read
// was started for:
//
//   - the read spoke for the seat's current session: the reading lands,
//     `unknown` included.
//   - the seat holds no session now: any old name is CLEARED, and a request
//     keeps only its `asked` half. A new session may run a different model,
//     and the old name would be a claim about a conversation that is gone.
//   - the seat holds a different session from the one the read looked up, or
//     one the read did not look up: the reading is STALE and the column is
//     left as it is. A newer read is already on its way, because every
//     dispatch that ends starts one.
func (m *Model) applySeatModels(msg seatModelMsg) {
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		requested := m.opts.Models[c.Vendor]
		now := m.sessions[c.Vendor]
		r, read := msg.readings[c.Vendor]
		switch {
		case now == "":
			c.Model = seatModelFor(requested)
		case read && r.session == now:
			c.Model = &SeatModel{Requested: requested, Read: true, Resolved: r.model}
		}
	}
}

// seatModelFor is the columns' starting state: the request alone, before any
// read. A seat with no request starts with nothing.
func seatModelFor(requested string) *SeatModel {
	if requested == "" {
		return nil
	}
	return &SeatModel{Requested: requested}
}

// refuseUnseatedModels refuses a model request for a seat the roster leaves
// out.
//
// Run calls it once the roster is decided (a typed --vendor, or the saved
// room's roster), before the alternate screen. A roster that names its seats
// and does not name this one will never spawn it, so the request would reach
// no argv while the command line said it was asked. That is a drop, and the
// rule is to refuse. The default roster (no list) seats every seat that can be
// driven, and a seat that cannot be driven says so on its own card.
func refuseUnseatedModels(seats Seats, models map[model.VendorID]string) error {
	if seats.All || len(seats.Only) == 0 {
		return nil
	}
	var out []string
	for v := range models {
		if !seats.names(v) {
			out = append(out, string(v))
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return fmt.Errorf("--model names %s, and this room seats only %s: add the seat with --vendor, or drop its model",
		strings.Join(out, ", "), seatList(seats.Only))
}

func seatList(vs []model.VendorID) string {
	s := make([]string, len(vs))
	for i, v := range vs {
		s[i] = string(v)
	}
	return strings.Join(s, ", ")
}

// errModelHosted is refuseHostedFlags' sentence for --model.
var errModelHosted = errors.New("--model is passed on the argv of each seat the room spawns, and a hosted room's seats are spawned by the host, which takes no model request yet")
