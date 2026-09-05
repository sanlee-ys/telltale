package council

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Styles is council's palette — the MONOGRAPH set (LEDGER, 2026-09-02, and the
// room-identity graft of 2026-09-03; see docs/room-identity.md).
//
// It no longer maps internal/theme's ANSI indices. The ledger lifted three taste
// rulings on 2026-09-02 — "council adds no hues of its own", "colour and any
// single glyph as only ever a second signal", and "Windows Terminal as the
// reference renderer" — and this file is what the first and third of them were
// holding shut. internal/theme is untouched, so internal/statusline and
// internal/hud keep the 4-bit palette and the stdlib-only rule (ADR-002) is
// unaffected: the blast radius of this set is one package.
//
// # Truecolour ENHANCES; it never DEFINES
//
// The 2026-09-03 audit ruled on this directly, against the prototype that made a
// private ink the carrier of a claim: an identity may spend a hex triple, and no
// distinction may DEPEND on one. So every ink below is redundant by
// construction — each thing it marks is already spelled by a word or a mark that
// survives --ascii, NO_COLOR and a 16-colour console — and the one ground this
// file paints (Palette.Rail) draws nothing at all when it is empty. That is the
// property the goldens prove rather than assert: they render PlainStyles, where
// the whole set is the identity function.
//
// # What replaced the ANSI indices, and why hex is now defensible
//
// theme.go argues that a hex triple is telltale asserting a colour over the
// scheme the user already chose, and that argument is correct for a STATUSLINE,
// which lives one line deep inside somebody else's prompt. The room is not that.
// It takes the whole terminal for as long as it runs, and a full-screen surface
// that inherits eight primaries from whatever scheme is loaded cannot have an
// identity — it can only have the terminal's. That is what "the room was correct
// and it was flat" (§9.11) was describing from the inside.
//
// # The ink scale, which is where the hierarchy actually lives
//
// SIX values of one ink, no hue, anchored on the terminal's own foreground. A
// monograph separates its levels by how much ink is on the page, not by how many
// colours are on it. So the eye is carried by VALUE (night figures shown):
//
//	Measured (#ece4d5 + weight)  the numbers, and the words that report one
//	Text     (the terminal's)    vendor prose — see Palette.Text
//	Muted    (#9a9081)           chrome, labels, the caps words
//	Dim      (#8a8174)           the reading area of a column the keys do not move
//	RuleInk  (#827869)           the ink rule: ━, and the composer's box
//	Hair     (#766c5f)           the hairline: ─, the separators, the leaders
//
// # The bottom three values were RAISED on 2026-09-03, and it was not taste
//
// The 2026-09-03 audit put this identity at a projector width and named the
// failure: at 180x50 "four unfocused columns recede too far; their prose and
// hairlines will disappear at the back." It was measurable. Against Campbell's
// #0c0c0c the first set drew Dim at 4.0:1, RuleInk at 3.2:1 and Hair at 1.9:1 —
// one below WCAG's 4.5:1 floor for body text and two below the 3:1 floor a
// non-text UI component needs, on a surface whose whole claim is that you can
// read it from across a room. The three values are now 5.0:1, 3.6:1 and 3.0:1,
// which clears both floors and keeps the order the scale depends on. The paper
// set moved for the same reason and by the same rule (PaperPalette).
//
// The top of the scale did NOT move. Measured, Muted, Identity, Withdrawn and
// Broke are the portfolio's own pigments and carry the portfolio's own measured
// figures; re-tinting them would have thrown away that provenance to fix a
// problem they did not have.
//
// # The two RULE inks were raised again on 2026-09-04, for the Zoom viewer
//
// The 2026-09-03 raise set the floor at the OWNER's screen. The demo audience
// is not on the owner's screen: it watches a Zoom share on a laptop, and the
// share is resampled on the way. A rule is one device pixel of a 21-pixel row,
// so the resample blends it with the ground and the ink loses contrast that the
// prose beside it does not. Measured on the emitted picture, at the recommended
// share mode: the hairline leader left the owner's screen at 3.1:1 and arrived
// at the viewer at 2.3:1, under the 3:1 floor a non-text component needs. The
// heavy rule did not lose the same amount, because ━ is a thick glyph and a
// thick glyph survives a resample.
//
// So the two rule inks now carry the resample's own cost as HEADROOM: Hair is
// 3.8:1 and RuleInk is 4.5:1 on Campbell, against 3.1 and 3.7 before, which is
// what puts both above 3:1 at the viewer rather than at the owner. Paper's Hair
// moved by the same rule and to the same figure (3.8:1); paper's RuleInk did
// not, because 10.5:1 already carries the headroom twice over.
//
// The order the scale depends on is unchanged and is now tighter: Muted 6.2,
// Dim 5.1, RuleInk 4.5, Hair 3.8. The gap between Dim and RuleInk is the one
// this raise spent, and it is accepted — prose and a rule never compete as the
// same kind of ink, and shape separates them before value does.
// TestTheRuleInksCarryTheZoomHeadroom is the gate; docs/room-identity.md's last
// section is the measurement.
//
// The one thing a reader is told to look at is the brightest ink on screen, and
// the several hundred lines a vendor emitted sit BELOW the six characters that
// say what it cost. That inversion IS the identity: this is an instrument, so
// the reading outranks the prose around it.
//
// # Two accents, and they are the owner's own
//
// The values come from the portfolio's MONOGRAPH landing (ADR-014 there), which
// is where this taste is already written down and already measured for contrast:
//
//	Identity (#9cb8d2 night / #1d4e73 paper)  ink blue: who, and what has focus
//	Withdrawn(#ffbe77 night / #8a4b12 paper)  copper: cancelled, unavailable, ⚠
//	Broke    (#e07a5f night / #5e1f0d paper)  oxide: failed, denied, a cut line
//
// The site spends exactly two accents — a fountain-pen blue for links, focus and
// what shipped, and an oxide/copper for what was taken back. The room needs a
// third because the site has no failure state and an instrument does: §4a.1 says
// a failure must be findable, and a failure that shared the withdrawn ink would
// not be. Oxide and copper are the same pigment at two strengths, which is what
// keeps the count honest — two families, not three.
//
// # What did NOT survive: five seat hues
//
// §9.28 ratified one 4-bit hue per seat and its own doc comment admitted the
// legal set was full at five, that two pairs were already twins a scheme could
// render close, and that "the tag is what scales; the hue was always going to run
// out." A room that answers "which seat" four times — by column position, by the
// seat NUMBER that is also its key, by the two-letter tag, and by the spelled-out
// name — does not need a fifth answer in colour, and the fifth is the one that
// turns a monograph into a sticker sheet. Seat identity is now ONE ink, and what
// separates the focused seat from the rest is weight (see seatInk).
//
// Weight and contrast still cost the palette nothing and still do the same jobs
// §9.27 gave them: Strong marks an anchor, Alert marks a claim a hurried reader
// must not skim, Dim recedes a column the keys are not in.
type Styles struct {
	Text     lipgloss.Style // vendor output, prompt text, the keys in the mode line
	Muted    lipgloss.Style // chrome, labels, de-emphasis
	Identity lipgloss.Style // vendor names, focused tab
	SevOK    lipgloss.Style // a column that finished cleanly
	SevWarn  lipgloss.Style // a column that is unavailable or cancelled
	SevCrit  lipgloss.Style // a column that failed

	// Measured is the brightest ink in the set, and it is spent on ONE class of
	// thing: a value telltale actually read off a vendor or off a process. The
	// cost figure, the clock a turn took, the token and quota cells, a diffstat,
	// an exit code's PASS or FAIL, the counts on a seat's rule.
	//
	// It is the render-side half of §4a.1. The honesty rule says a value that was
	// not measured is absent rather than plausible; this says that a value that
	// WAS measured is the darkest ink on the page. Nothing inferred and nothing
	// estimated may wear it — an estimate already carries its leading `~`, and
	// the point of that mark is that it is not this.
	//
	// PlainStyles renders it as the identity function, like every other token
	// here, so no layout golden can see it.
	Measured lipgloss.Style

	// Hair and RuleInk are the room's two rule weights, given ink to match the
	// two glyphs §9.26 already spends (g.Rule and g.RuleHeavy).
	//
	// The room drew both at one intensity, so the weight distinction lived
	// entirely in a single character's stroke — which is exactly as much
	// hierarchy as a reader at the back of a room can resolve, i.e. none. A
	// printed page uses a hairline for a row and an ink rule for a section, and
	// the difference between them is ink as much as it is width.
	//
	// Hair draws the column separators, the header's zone bar, and the leader
	// that fills a column header. RuleInk draws the full-bleed ━ under the header
	// and the composer's box — the two things that say where the room's chrome
	// stops.
	Hair    lipgloss.Style
	RuleInk lipgloss.Style

	// Focus is the ink on the one vertical mark that is as tall as the thing it
	// describes: the rail left of the column the keys move (§9.27, g.FocusRail).
	//
	// The rail was Muted, which put it at the same intensity as the three plain
	// separators beside it and left the whole signal riding on one character's
	// extra stroke. Blue is the site's focus ink; this is the one place the room
	// spends it on something that is not a name.
	Focus lipgloss.Style

	// Strong is Identity at full weight: the room's own name, each seat's name,
	// and the user's brief echoed back inside a column. Everything it marks is
	// an ANCHOR — the thing a reader is looking for when they scan the frame —
	// rather than a value that changed this second.
	Strong lipgloss.Style
	// Alert is SevWarn at full weight, and it is spent only on claims a hurried
	// reader must not skim past. Every one already carries its meaning in words
	// — the weight only makes the word findable. Its spenders: the posture
	// badge saying this seat can change your files (ForSandbox), the title line
	// of a card that explains a seat that is not working or asks for a decision
	// (unavailableCard, gateCardLines), the needs-you strip and the seat names
	// on it while a seat on it is BLOCKED (§9.40; an inbox of landed replies
	// takes Strong instead, see unreadLead), and the composer's GATE word —
	// each a state where the reader is the thing the room is waiting on. This
	// list was "exactly two things" once and drifted three users behind; if you
	// spend Alert somewhere new, add it here.
	Alert lipgloss.Style

	// Dim is Text one contrast step down, and it is spent on exactly one thing:
	// the reading area of a column the keys do NOT move (§9.27).
	//
	// The room's focus signals — `▸`, the seat name's weight, and now the thick
	// rail — all say which column is addressed. None of them says anything about
	// the other three, so four columns of prose arrived at one intensity and the
	// eye had no reason to land anywhere. This is crush's Focused/Blurred pair
	// applied to prose rather than to a border: the addressed column keeps full
	// contrast and its neighbours recede.
	//
	// It is Text + Faint, which is Muted's own attribute, and that collapse is
	// ACCEPTED rather than overlooked. Inside an unfocused column the prose and
	// the chrome do end up at one intensity — but every distinction between them
	// is carried by shape first: a turn separator is a labelled rule, a trace
	// entry opens with ⚙, a skip line opens with ○, a note with ⚠. Weight was
	// always the second signal there (§7.1 rule 2), so what is lost is the second
	// signal on a column the reader is not reading. Widening this to the focused
	// column's chrome, or to any card, would spend it where the second signal is
	// still doing work.
	//
	// PlainStyles renders it as the identity function, so no layout golden sees
	// it and the distinction it makes is one `▌` and `▸` already carry whole
	// under --ascii and NO_COLOR.
	Dim lipgloss.Style

	// Blurred marks a derived set for a column the keys do not move — see
	// forSeat. It exists so the prose builders can ask ONE question (`sty.Body()`)
	// rather than each taking a seatFocus they would each have to interpret.
	Blurred bool

	// RailGround is the ground the POSTURE RAIL is printed on, or "" for no rail
	// at all (PlainStyles, and any set that does not want one).
	//
	// The rail is the 2026-09-03 graft's one new primitive, and it is the
	// prototype the audit picked as the best posture ledger of the three: the
	// badge row — the row that says what each seat is ALLOWED to do — is painted
	// as one continuous horizontal object that runs the whole frame, gutters and
	// frame pads included, rather than as five separate cells. Two things follow
	// from that, and only the second is the reason it is worth having.
	//
	// The ledger becomes the frame's governing object, so the eye goes to the
	// differentiator instead of to the prose. And an ABSENT reading stops being
	// an ambiguous blank: on the rail the same nothing is a gap in a printed
	// line — visibly part of the ledger, visibly holding no reading, and visibly
	// not a zero, because a measured zero prints `$0.0000` in the measured ink
	// and an unmeasured one prints nothing at all. That is §4a.1's
	// zero-versus-absent rule drawn rather than written, and it costs no
	// character and no golden.
	//
	// It is REPAINTED from the prototype's warm band into this palette's own
	// values (Palette.Rail): the ground is the bottom of the same ink scale
	// every other token comes from, one step off the terminal's own, so the rail
	// reads as the paper the readings are printed on rather than as a coloured
	// stripe laid over the room.
	//
	// **Nothing depends on it.** Empty here — which is what PlainStyles hands
	// out, and what a set with no ground would — and onBand returns its argument
	// untouched, so the whole rail vanishes and every claim on it is still
	// spelled by its own word. That is the audit's "truecolour may enhance,
	// never define" honoured at the type level rather than by inspection.
	RailGround string

	// Plain reports that every style is a no-op, for layout goldens.
	Plain bool
}

// Palette is one MONOGRAPH ink set, resolved to hex.
//
// A value type rather than two constructors so that the light set and the dark
// set are the same object with different pigments — a token that exists in one
// and not the other is a compile error rather than a surface that renders in
// only one theme.
type Palette struct {
	Measured string // the reading: numbers and the words that report one

	// Text is EMPTY in both palettes, and that is the surviving half of
	// theme.go's argument rather than an omission.
	//
	// Vendor prose renders in the terminal's own foreground. Two reasons, and
	// the second is mechanical. First: the room supplies structure and accents,
	// and the base ink a reader has already chosen for every other program is
	// the right ink for several hundred lines somebody else's model wrote.
	// Second: the render path leaves a number of body lines BARE — a commit
	// receipt, a diffstat, an arena error — and a Text that were a hex would put
	// those lines at a colour the palette never named, which is a divergence no
	// golden can see (they all render PlainStyles) and every terminal would.
	//
	// The consequence is stated plainly: the ink scale is anchored at the
	// terminal's foreground, and every other token is positioned ABOVE it
	// (Measured) or BELOW it (Muted, Dim, RuleInk, Hair).
	Text string

	Muted     string // chrome and labels
	Dim       string // a column the keys do not move
	RuleInk   string // ━ and the composer's box
	Hair      string // ─, the separators, the leaders
	Identity  string // ink blue: who, and what has focus
	Withdrawn string // copper: cancelled, unavailable, the ⚠ mark
	Broke     string // oxide: failed, denied, a line the patch cut

	// Rail is the one value in this set that is a GROUND rather than an ink: the
	// paper the posture ledger is printed on (Styles.RailGround).
	//
	// It is the bottom of the same warm scale as the rest — below Hair on the
	// night set, above Hair on the paper one — so it belongs to the page rather
	// than being laid over it. Quiet on purpose: what makes the rail carry the
	// eye is that it is the one continuous horizontal object on the frame, not
	// that it is loud, and a saturated band would out-shout the accents two rows
	// under it.
	//
	// Every ink the badge row spends is legible on it, and that is asserted
	// rather than eyeballed — see TestTheRailIsLegibleOnBothGrounds.
	Rail string
}

// NightPalette is the room after dark, with a lamp on — the theme a terminal is
// nearly always in.
//
// It is NOT the paper set inverted. A dark theme built by flipping a warm light
// one lands on a cold blue-black, because the eye reads "dark" as "cool" and the
// tokens follow; that throws away the reason the ground was warm. Ink is bone
// rather than white, and both accents are lifted to the values they need at low
// luminance rather than the values they had on paper.
//
// Contrast, and where each figure comes from. Measured 15.0:1, Muted 6.0:1,
// Identity 9.0:1 and Withdrawn 11.4:1 are the PORTFOLIO's own measurements,
// taken there against its #16130f ground; they are copied rather than re-derived
// because the pigments are copied. Against Windows Terminal's Campbell ground
// (#0c0c0c) every one of them is higher, that ground being darker still, so the
// figures above are a floor rather than a claim about this surface.
//
// Broke is the one value not taken from the site — the site has no failure
// state — so its figure is computed here rather than cited: #e07a5f is 7.0:1 on
// #0c0c0c. It is the same oxide pigment as Withdrawn, lifted for the lamp, which
// is what keeps the accent count at two families rather than three.
//
// Nothing here paints a background. The ground stays whatever terminal the
// operator chose, which is the half of theme.go's argument that survives intact:
// the room supplies ink, not paper.
func NightPalette() Palette {
	return Palette{
		Measured: "#ece4d5",
		Text:     "",
		Muted:    "#9a9081",
		// The three raised on 2026-09-03 — 5.1:1, 3.7:1 and 3.1:1 on #0c0c0c,
		// against 4.0, 3.2 and 1.9 before. See the projector paragraph on
		// Styles. Every figure here is computed against Campbell rather than
		// cited, because these three are this room's own pigments and not the
		// site's.
		//
		// The two RULE inks were raised again on 2026-09-04, to 4.5:1 and
		// 3.8:1, so that a one-pixel mark still clears 3:1 AFTER a Zoom share
		// resamples it. Dim did not move: prose is many pixels thick and it
		// loses almost nothing to the resample. See the Zoom paragraph on
		// Styles.
		Dim:     "#8a8174",
		RuleInk: "#827869",
		Hair:    "#766c5f",
		// 1.25:1 against the same ground: a band a reader SEES and does not
		// READ. Every ink the badge row spends clears WCAG AA on it (Muted, the
		// quietest of them, at 4.98:1), which is the constraint that picked this
		// value rather than the two steps brighter it started at.
		Rail:      "#26231d",
		Identity:  "#9cb8d2",
		Withdrawn: "#ffbe77",
		Broke:     "#e07a5f",
	}
}

// PaperPalette is the same room in daylight, for a light terminal.
//
// The two accents change character rather than just value, which is the one
// thing a light set cannot skip: oxide red is what an editor's pencil leaves on
// a page, and copper at this luminance would read as highlighter. So the paper
// set takes the pigments down — burnt sienna for withdrawn, oxide for broke —
// while the night set takes them up.
//
// Measured, Muted, Identity and Broke are the site's own paper values and carry
// its measurements (16.25:1, 6.37:1, 8.41:1 and 11.98:1 against #fafaf9).
// Withdrawn is the value computed here, for the same reason Broke is at night:
// the site spends ONE warm pigment and the room needs two strengths of it.
// #8a4b12 is 6.8:1 on white, so it clears WCAG AA for body text — which it has
// to, because this is a badge that says a seat may change your files.
func PaperPalette() Palette {
	return Palette{
		Measured: "#1e1c1b",
		Text:     "",
		Muted:    "#5f5c58",
		// Darkened on 2026-09-03 for the projector, exactly as the night set's
		// three were lightened: Dim was 3.6:1 on white and Hair was 1.5:1, and a
		// hairline at 1.5:1 is a rule that exists in a screenshot and not in a
		// room. Dim is 5.4:1 now, and it stays LIGHTER than Muted (6.65:1),
		// which is the ordering the unfocused column depends on. RuleInk did not
		// move: on paper the ink rule is meant to be the heaviest line on the
		// page, and 10.5:1 is already that.
		//
		// Hair was darkened again on 2026-09-04, from 3.1:1 to 3.8:1, for the
		// Zoom viewer rather than for the projector — a one-pixel mark loses
		// contrast when the share is resampled, and 3.1:1 arrived under the 3:1
		// floor. RuleInk needed nothing here; 10.5:1 carries the headroom twice
		// over.
		Dim:     "#6f6a64",
		RuleInk: "#423f3c",
		Hair:    "#86837a",
		// 1.29:1 on white — the same barely-there band as the night set's, on
		// the other side of the ink scale, and every badge ink clears WCAG AA on
		// it (Muted at 5.15:1).
		Rail:      "#e8e2d6",
		Identity:  "#1d4e73",
		Withdrawn: "#8a4b12",
		Broke:     "#5e1f0d",
	}
}

// NewStyles builds the coloured set. isDark now actually decides something: it
// picks the palette, and the two are different objects rather than one palette
// with a brightness knob (see PaperPalette).
func NewStyles(isDark bool) Styles {
	p := PaperPalette()
	if isDark {
		p = NightPalette()
	}
	ink := func(hex string) lipgloss.Style {
		if hex == "" {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	}
	return Styles{
		Text:     ink(p.Text),
		Muted:    ink(p.Muted),
		Identity: ink(p.Identity),
		// A finished turn reports a measurement — the process exited, and the
		// room read how it exited — so `✓ done` and `PASS` wear the reading's own
		// ink rather than a hue of their own. That is one fewer colour than the
		// green this replaced, and it says something the green did not: this is a
		// fact somebody measured.
		SevOK:   ink(p.Measured).Bold(true),
		SevWarn: ink(p.Withdrawn),
		SevCrit: ink(p.Broke),
		// Strong is the identity ink at weight. It marks an ANCHOR — the room's
		// own name, the seat the keys move, the brief echoed back — which is the
		// same job the site's blue does for a link and for focus.
		Strong: ink(p.Identity).Bold(true),
		// Alert is the withdrawn ink at weight, and its spenders are unchanged:
		// a posture badge saying this seat can change your files, a card that
		// asks for a decision, the needs-you strip, the composer's GATE word.
		Alert: ink(p.Withdrawn).Bold(true),
		Dim:   ink(p.Dim),
		// Ink AND weight. The ink alone carries it on a dark ground, where bone
		// is plainly above the terminal's own grey; on paper the room's base ink
		// IS near-black and a second near-black would be no signal at all, so the
		// number is set in weight the way a table of figures is set in print. One
		// token, one appearance, both grounds — a Measured that meant "brighter"
		// in one theme and "bolder" in the other would be two tokens.
		Measured:   ink(p.Measured).Bold(true),
		Hair:       ink(p.Hair),
		RuleInk:    ink(p.RuleInk),
		Focus:      ink(p.Identity),
		RailGround: p.Rail,
	}
}

// onBand puts a style on the posture rail's ground.
//
// A method rather than a second set of fields, so PlainStyles stays the identity
// set BY CONSTRUCTION: the guard here returns the style untouched and no golden
// can see the rail exist. A parallel `RailMuted`, `RailAlert`, … would have to
// remember that, and would forget it the first time one of them grew an
// attribute.
func (s Styles) onBand(st lipgloss.Style) lipgloss.Style {
	if s.Plain || s.RailGround == "" {
		return st
	}
	return st.Background(lipgloss.Color(s.RailGround))
}

// bandFill is the rail's ground with no text style on it — what the EMPTY half
// of a ledger cell is painted with, and the whole reason the rail is worth
// having (see Styles.RailGround).
func (s Styles) bandFill() lipgloss.Style {
	return s.onBand(lipgloss.NewStyle())
}

// onRail is the whole set moved onto the rail's ground, for a builder that
// renders INTO the badge row from another file (seatQuotaCell).
//
// A derived set rather than a ground threaded through that builder, for forSeat's
// own reason: the change has to reach a helper two calls deep, every one of them
// already takes a Styles, and passing the ground alongside would mean each of
// them deciding for itself what "on the rail" means.
func (s Styles) onRail() Styles {
	if s.Plain || s.RailGround == "" {
		return s
	}
	s.Text = s.onBand(s.Text)
	s.Muted = s.onBand(s.Muted)
	s.Identity = s.onBand(s.Identity)
	s.SevOK = s.onBand(s.SevOK)
	s.SevWarn = s.onBand(s.SevWarn)
	s.SevCrit = s.onBand(s.SevCrit)
	s.Strong = s.onBand(s.Strong)
	s.Alert = s.onBand(s.Alert)
	s.Dim = s.onBand(s.Dim)
	s.Measured = s.onBand(s.Measured)
	return s
}

// Lane is the REACHED part of a racer's lane track and Track is the part it has
// not reached (the arena's race board — arenalane.go).
//
// The fill is the racer's host-observed finishing position, so it takes the ink
// this room spends on a figure somebody measured; the rest of the track is a
// hairline, which is what the room draws a distance in. Neither is a seat's own
// colour: seat hues were retired with §9.28 and a lane that wore one would be
// this identity buying back the thing it sold.
func (s Styles) Lane() lipgloss.Style  { return s.Measured }
func (s Styles) Track() lipgloss.Style { return s.Hair }

// PlainStyles is the identity set: every Render returns its input unchanged, so
// layout goldens compare byte for byte without depending on the CI terminal's
// colour profile.
func PlainStyles() Styles {
	s := lipgloss.NewStyle()
	return Styles{
		Text: s, Muted: s, Identity: s,
		SevOK: s, SevWarn: s, SevCrit: s,
		// Strong and Alert are identity here too, which is the whole reason
		// weight is safe to introduce: it changes no cell's width and no line's
		// content, so every layout golden is blind to it and every distinction
		// it makes is one the words and glyphs already carried.
		Strong: s, Alert: s,
		// Dim likewise: an unfocused column's prose is the same bytes at the same
		// width, which is what makes contrast safe to spend as a signal at all.
		Dim: s,
		// The MONOGRAPH tokens are no-ops here for the same reason and it is the
		// whole cost model of this identity: every one of them is a colour or a
		// weight, so the room's bytes are the bytes the goldens already hold.
		Measured: s, Hair: s, RuleInk: s, Focus: s,
		// RailGround is left empty, which is the rail switched OFF — onBand
		// returns its argument untouched, fitOn pads in plain spaces, and the
		// posture row is byte-for-byte the row it was before the rail existed.
		// A golden cannot see the ledger's ground, and no claim on it needs one.
		Plain: true,
	}
}

// Rule is the hairline: the column separators, the header's zone bar, and the
// leader that fills a column header out to its state word.
func (s Styles) Rule() lipgloss.Style { return s.Hair }

// RuleStrong is the ink rule: the full-bleed ━ where the room's chrome stops and
// the seats begin, and the composer's box.
//
// Two weights of rule, and the second one now differs by INK as well as by
// stroke. §9.26 bought the distinction with a character; a reader at a projector
// width at the back of a room cannot resolve a character's stroke, and can
// resolve which line is darker.
func (s Styles) RuleStrong() lipgloss.Style { return s.RuleInk }

// forSeat derives the set one column renders with: unchanged for the seat the
// keys move, one contrast step down for every other (§9.27).
//
// A derived SET rather than a seatFocus threaded through columnLines, because
// the demotion has to reach builders nested three deep and every one of them
// already takes a Styles. Passing the focus alongside would mean each of them
// deciding for itself what "unfocused" means, which is exactly how a rule this
// narrow gets widened by accident.
func (s Styles) forSeat(f seatFocus) Styles {
	if f.hasKeys() {
		return s
	}
	s.Blurred = true
	return s
}

// Body is the style a column's READING AREA renders in: the vendor's reply, the
// live stand-in for one that has not arrived yet, and the line standing in for a
// seat that has not been asked anything.
//
// Not the chrome around it (already Muted). Not the user's echoed brief, which
// stays Strong in every column — what a seat was ASKED is the thing a reader
// scrolls looking for, and it is the user's own words rather than a vendor's.
// Not a note or a card: those carry failure and posture claims, and §9.2's rule
// is that a claim does not get quieter because the reader is looking elsewhere.
func (s Styles) Body() lipgloss.Style {
	if s.Blurred {
		return s.Dim
	}
	return s.Text
}

// bold adds weight without breaking the identity set.
//
// PlainStyles has to stay a true no-op — every layout golden compares bytes —
// and lipgloss will happily emit a bold escape from an otherwise empty style,
// so weight is applied through here rather than by calling Bold in the
// renderer.
func (s Styles) bold(st lipgloss.Style) lipgloss.Style {
	if s.Plain {
		return st
	}
	return st.Bold(true)
}

// ForPhase returns the style a column's status word renders in.
//
// Idle, waiting and streaming are all Muted: a column in flight is not a
// severity, and colouring it would spend the alphabet's loudest signal on the
// most common state.
func (s Styles) ForPhase(p Phase) lipgloss.Style {
	switch p {
	case PhaseDone:
		return s.SevOK
	case PhaseFailed:
		return s.SevCrit
	case PhaseCancelled:
		return s.SevWarn
	default:
		return s.Muted
	}
}

// ForSandbox returns the style a column's posture badge renders in.
//
// §9.2 makes the badge a safety claim and then leaves it looking like every
// other word on a faint line: on a room where one seat is `ro:tools` and the
// one beside it can edit your working tree, both whispered at the same volume.
// The badge that says a seat may CHANGE things now carries weight and the
// warning hue; the ones that name a read-only mechanism stay chrome.
//
// This does not weaken the rule that colour is redundant — it strengthens it.
// The distinction is still carried entirely by the word (`unsandboxed` and
// `WRITES` break the `ro:` prefix on purpose), which is why the badges survive
// --ascii and NO_COLOR exactly as they did. Weight and hue only make the word
// findable in a frame with four columns of prose in it.
//
// # The ladder is ordered by EVIDENCE, not by risk (2026-09-03)
//
// The three read-only badges all rendered Muted — one volume for `ro:enforced`,
// `ro:tools` and `ro:requested` — and those three are not one fact. state.go
// says so in its own doc comments: `ro:enforced` is an OS-level sandbox this
// repo drove and measured at a pinned version, `ro:tools` is a restricted tool
// set read off what the session reported about itself, and `ro:requested` is a
// flag that was passed and accepted with NOTHING measuring the result. The
// posture ledger is the thing no competitor draws, and it was drawn in one
// colour — so the difference between a containment this project MEASURED and one
// it merely ASKED FOR arrived at the eye as chrome. §4a.1 says an unmeasured
// value is either omitted or visibly marked; this is that rule reaching the one
// surface the product exists for.
//
// Five distinct renders, strongest evidence first:
//
//	ro:enforced   SevOK       an OS sandbox, driven and measured at a version
//	ro:tools      Text        a real mechanism, named, at full contrast
//	ro:requested  SevWarn     asked for; nothing measured what came back
//	gated         Text + bold may write, and has to be told yes first
//	WRITES        Alert       may change your files, right now
//	unsandboxed   Alert       has no read-only posture at all, and that is measured
//
// SevOK on `ro:enforced` is the one to argue for. In this palette SevOK IS the
// measured ink at weight (NewStyles), so it does not say "fine" — it says "a
// value somebody read", which is exactly the claim `ro:enforced` makes and
// exactly what separates it from the badge below it. Minting a sixth token for
// "measured" would be the drift this file forbids.
//
// `WRITES` and `unsandboxed` share Alert deliberately: they are two routes to
// one consequence, the word is what says which, and splitting them by ink would
// spend the loudest treatment in the room on a distinction that changes nothing
// a reader would do next.
//
// None of this weakens the redundancy rule. Every step is spelled by a DIFFERENT
// WORD, the words are what --ascii and NO_COLOR read, and PlainStyles renders
// all six as the identity style, so no golden can see any of it.
func (s Styles) ForSandbox(l SandboxLevel) lipgloss.Style {
	switch l {
	case SandboxWrite, SandboxNone:
		return s.Alert
	case SandboxGated:
		// Bold, but not a severity. A gated seat is the room working: it may do
		// everything SandboxWrite allows and has to be told yes first, so
		// colouring it like the ungated ones would teach the eye to skip the
		// difference the gate exists to make.
		return s.bold(s.Text)
	case SandboxEnforced:
		return s.SevOK
	case SandboxTools:
		// Full contrast, no ink of its own. A tool allowlist is a real mechanism
		// and it is not an OS boundary; it sits between the two claims that DO
		// carry ink, and the absence of one is what says so.
		return s.Text
	case SandboxRequested:
		return s.SevWarn
	default:
		return s.Muted
	}
}

// seatInk is the ONE ink every seat name renders in — the retirement of §9.28's
// five-hue exception, under the 2026-09-02 ledger ruling that lifted "council
// adds no hues of its own" and, with it, the frame that exception was carved
// out of.
//
// **What the exception bought and what it cost.** §9.28 spent one 4-bit index
// per seat so a reader could sort names by colour. Its own doc comment then
// recorded the bill: the legal set (4, 5, 6, 12, 13, 14) held six indices for
// five seats; agy/cursor and codex/grok were already twins that some schemes
// render close; and the note ended "the tag is what scales; the hue was always
// going to run out."
//
// **Why one ink is the better answer, not the cheaper one.** The room answers
// "which seat" four times before colour is reached: the column's POSITION, the
// seat NUMBER that is also the key that reaches it, the two-letter TAG, and the
// spelled-out NAME. §9.18 and §9.29 built that ladder deliberately, and it is
// the ladder that survives --ascii, NO_COLOR, a projector and a colour-blind
// reader. A fifth answer in hue is redundancy on the one question the room had
// already over-answered, and five saturated hues on one screen is what makes a
// terminal read as a sticker sheet rather than as a page.
//
// **What still separates seats, and it is stronger than before.** Weight. The
// seat the keys move renders SeatStrong (the identity ink at weight); every
// other renders SeatIdentity (the same ink, plain). That is the site's own law —
// blue marks focus — and, unlike a hue, it says which of the five you are IN,
// which is the question a reader in a five-column room actually asks.
//
// **What this gives back.** A sixth vendor needs no hue decision at all. The
// question §9.28 left as "now the HARD one" simply stops existing.
func (s Styles) seatInk(st lipgloss.Style) lipgloss.Style { return st }

// SeatIdentity is the ink a seat's name renders in, and SeatStrong is the same
// ink at weight — the seat the keys move.
//
// They stay two named methods rather than collapsing into Identity and Strong at
// every call site, because the CALL SITES are the closed list §9.28 built and
// TestTheInkIsSpentOnlyOnSeatNames still reads: a seat rule on a turn page, a
// tab, a seat named inside the collapsed-seat notice. Keeping the name keeps the
// list checkable by grep, and keeps one place to change if a future ruling wants
// per-seat ink back.
func (s Styles) SeatIdentity(v model.VendorID) lipgloss.Style {
	_ = v
	return s.seatInk(s.Identity)
}

// SeatStrong is a seat's name at weight, where seat names are what the eye is
// sorting and one of them is the one the keys move.
func (s Styles) SeatStrong(v model.VendorID) lipgloss.Style {
	_ = v
	return s.seatInk(s.Strong)
}

// ForDiffLine returns the style one raw patch line renders in — the "later,
// separate change" §9.37's `d` amendment left on the table, landed entirely
// inside the palette the room already has. Added lines wear SevOK, removed
// lines SevCrit, headers the Muted chrome style; nothing here is a new hue,
// and nothing here is a first signal. The `+`/`-` prefixes carry the whole
// distinction on their own — they are what survives --ascii and NO_COLOR, and
// under PlainStyles every branch of this switch is the identity style, which
// is why no layout golden can see this function exist.
//
// Classification reads the RAW line, and the headers are matched FIRST, in
// this order, because three of them are prefix-shadowed by the change
// markers: `+++ b/file` opens with the addition's own `+`, `--- a/file` with
// the removal's `-`, and a classifier that checked `+` before `+++` would
// paint a file header as an inserted line — a header wearing green is the
// patch claiming a change it never made. `index ` keeps its trailing space on
// purpose: a body line of prose can begin with the word "index", and only
// git's own `index 1234..5678 100644` form carries the space-delimited shape.
//
// SevOK/SevCrit here are severity tokens spent on non-severity facts, and
// that reuse is deliberate rather than sloppy: green-for-added and
// red-for-removed is the one colour convention every diff reader already
// owns, it is the same green/red every terminal diff pager maps to these same
// ANSI indices, and minting a second green would be exactly the sixth-colour
// drift the package comment forbids. The context lines stay Text — a line the
// patch did not touch is the baseline the coloured lines read against.
func (s Styles) ForDiffLine(line string) lipgloss.Style {
	switch {
	case strings.HasPrefix(line, "+++"),
		strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff --git"),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "@@"):
		return s.Muted
	case strings.HasPrefix(line, "+"):
		return s.SevOK
	case strings.HasPrefix(line, "-"):
		return s.SevCrit
	default:
		return s.Text
	}
}

// ForAvailability returns the style an unavailable column's card renders in.
func (s Styles) ForAvailability(a Availability) lipgloss.Style {
	if a == AvailInstalled {
		return s.Text
	}
	return s.SevWarn
}
