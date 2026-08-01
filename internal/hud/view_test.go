package hud

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/model"
)

var update = flag.Bool("update", false, "rewrite the golden renders")

// pinned is the clock every golden renders against. View() never calls
// time.Now, so a fixed instant makes every frame reproducible.
var pinned = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------- fixtures
//
// All data below is SYNTHESIZED: fake session ids, fake names, fake paths.
// This repo is public and real transcripts carry private material.

func ago(d time.Duration) *time.Time { return model.TimePtr(pinned.Add(-d)) }

type sessionOpt func(*model.Session)

func withCtx(p float64) sessionOpt {
	return func(s *model.Session) { s.ContextPercent = model.PercentPtr(p) }
}
func withCost(c float64) sessionOpt { return func(s *model.Session) { s.Cost = model.USDPtr(c) } }
func withName(n string) sessionOpt  { return func(s *model.Session) { s.Name = model.Ptr(n) } }

func derived() sessionOpt {
	return func(s *model.Session) { s.Derived = s.Derived.With(model.FieldContextPercent) }
}

func degraded(f model.Field) sessionOpt {
	return func(s *model.Session) { s.Degraded = s.Degraded.With(f) }
}

func noActivity() sessionOpt { return func(s *model.Session) { s.LastActivity = nil } }

func withQuota(w ...model.QuotaWindow) sessionOpt {
	return func(s *model.Session) { s.Quota = append(s.Quota, w...) }
}

func sess(vendor model.VendorID, id, workspace, modelID string, age time.Duration, opts ...sessionOpt) *model.Session {
	s := &model.Session{
		Vendor:       vendor,
		ID:           id,
		ObservedAt:   pinned,
		LastActivity: ago(age),
	}
	if workspace != "" {
		s.WorkspaceDir = model.Ptr(workspace)
	}
	if modelID != "" {
		s.Model = &model.Model{ID: modelID}
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func window(id, label string, used float64, resets time.Duration) model.QuotaWindow {
	return model.QuotaWindow{
		ID:          id,
		Label:       label,
		UsedPercent: model.PercentPtr(used),
		ResetsAt:    model.TimePtr(pinned.Add(resets)),
	}
}

// fullCaps is a SYNTHETIC vendor that can source everything. The grid renders
// A–F with it so every column is exercised; no real v1 adapter has this
// capability set, and the "v1-capabilities" golden shows what the real mix
// actually renders.
var fullCaps = model.Capabilities{
	Reported: model.NewFieldSet(
		model.FieldName, model.FieldModel, model.FieldWorkspace,
		model.FieldContextPercent, model.FieldCost, model.FieldQuota,
		model.FieldLastActivity,
	),
}

func watching(v model.VendorID, root string, caps model.Capabilities) VendorView {
	return VendorView{Vendor: v, Root: root, Status: StatusWatching, Caps: caps}
}

// healthy is the reference data set: three Claude sessions and one Codex
// session, spanning live / idle / stale and one row with no context or cost.
func healthy() []*model.Session {
	return []*model.Session{
		sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
			`C:\src\code\telltale`, "claude-opus-5", 12*time.Second,
			withName("telltale"), withCtx(84.2), withCost(2.41),
			withQuota(
				window("five_hour", "5h", 42, 2*time.Hour+13*time.Minute),
				window("seven_day", "7d", 18, 5*24*time.Hour+2*time.Hour),
			)),
		sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
			`C:\src\work\acme-api`, "claude-sonnet-4-5", 48*time.Second,
			withName("acme-api"), withCtx(41), withCost(0.18)),
		sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000001",
			`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute),
		sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000003",
			`C:\src\code\learning-notes`, "claude-haiku-4-5", 22*time.Minute,
			withName("learning-notes"), withCtx(92.6), withCost(11.07)),
	}
}

func healthyState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		Sessions: healthy(),
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
		},
		At: pinned,
	}
	return st
}

// ------------------------------------------------------------------ goldens

func TestGoldenRenders(t *testing.T) {
	cases := []struct {
		name  string
		state func() State
		ascii bool
	}{
		{name: "wide-healthy", state: func() State { return healthyState(120, 9) }},
		{name: "compact", state: func() State { return healthyState(80, 10) }},
		{name: "narrow", state: func() State { return healthyState(72, 10) }},
		{name: "floor-width", state: func() State { return healthyState(52, 9) }},
		{name: "floor-height", state: func() State { return healthyState(120, 4) }},
		{name: "ascii", ascii: true, state: func() State { return healthyState(120, 9) }},

		{name: "filter-sort", state: func() State {
			st := healthyState(120, 9)
			st.Filter = FilterClaude
			st.Sort = SortContext
			return st
		}},

		{name: "help", state: func() State {
			st := healthyState(120, 11)
			st.Help = true
			return st
		}},

		// Values are the last ones actually measured; the row area drops to
		// Muted and the footer carries the notice. Plain styles make that
		// invisible in a layout golden, so TestStaleScanDimsTheRowArea asserts
		// the styling separately.
		{name: "stale-scan-47s", state: func() State {
			st := healthyState(120, 9)
			st.Snap.At = pinned.Add(-47 * time.Second)
			st.Snap.Err = "Access is denied."
			return st
		}},
		{name: "stale-scan-90s", state: func() State {
			st := healthyState(120, 9)
			st.Snap.At = pinned.Add(-90 * time.Second)
			return st
		}},

		// Four failure shapes in one frame.
		{name: "degraded", state: func() State {
			st := healthyState(120, 9)
			st.Snap.Sessions = []*model.Session{
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
					`C:\src\code\telltale`, "claude-opus-5", 3*time.Second,
					withName("telltale"), withCtx(0), withCost(0.04),
					withQuota(window("five_hour", "5h", 42, 2*time.Hour+13*time.Minute))),
				// Discovered by filename; its only record was torn, so nothing
				// parsed and the label falls back to the session id.
				sess(model.VendorCodex, "4f2a9c81-1d3e-4a77-9b02-000000000000",
					"", "", 7*time.Minute),
				// A record timestamp in the future: no readable age at all.
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
					`C:\src\work\acme-api`, "claude-sonnet-4-5", 0,
					withName("acme-api"), withCost(1.02), noActivity(),
					degraded(model.FieldLastActivity), degraded(model.FieldContextPercent)),
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000003",
					`C:\src\code\overflow`, "claude-opus-5", 9*time.Second,
					withName("a-really-long-project-name-that-overflows-the-label-column-and-then-some"),
					withCtx(99.9), withCost(340.50)),
			}
			return st
		}},

		// THE load-bearing assertion: 0% is a full track, absent is whitespace.
		{name: "zero-vs-absent", state: func() State {
			st := healthyState(120, 9)
			st.Snap.Sessions = []*model.Session{
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
					`C:\src\code\at-zero`, "claude-opus-5", 5*time.Second,
					withName("at-zero"), withCtx(0), withCost(0)),
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
					`C:\src\code\no-source`, "claude-opus-5", 6*time.Second,
					withName("no-source")),
			}
			return st
		}},

		// Every visible row lacks context and cost, so both columns are
		// dropped and their width returns to SESSION.
		{name: "column-hidden", state: func() State {
			st := healthyState(120, 9)
			st.Snap.Sessions = []*model.Session{
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
					`C:\src\code\telltale`, "claude-opus-5", 12*time.Second, withName("telltale")),
				sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000001",
					`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute),
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000003",
					`C:\src\code\learning-notes`, "claude-haiku-4-5", 22*time.Minute,
					withName("learning-notes")),
			}
			return st
		}},

		// An API-key login on Claude and no Codex: sessions exist, quota does
		// not. The header block is ABSENT, never "5h 0%".
		{name: "quota-absent", state: func() State {
			st := healthyState(120, 9)
			st.Snap.Sessions = []*model.Session{
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
					`C:\src\code\telltale`, "claude-opus-5", 12*time.Second,
					withName("telltale"), withCtx(84.2), withCost(2.41)),
				sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
					`C:\src\work\acme-api`, "claude-sonnet-4-5", 48*time.Second,
					withName("acme-api"), withCtx(41), withCost(0.18)),
			}
			return st
		}},

		// What v1 ACTUALLY renders, using the real adapters' declared
		// capabilities: Claude sources neither context nor cost from disk;
		// Codex sources a derived context percentage and real quota windows.
		{name: "v1-capabilities", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 9
			st.Snap = Snapshot{
				At: pinned,
				Sessions: []*model.Session{
					sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
						`C:\src\code\telltale`, "claude-opus-5", 12*time.Second,
						withName("telltale")),
					sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000002",
						`C:\src\code\example-app`, "gpt-5.1-codex", 90*time.Second,
						withCtx(189888.0/272000.0*100), derived(),
						withQuota(window("primary", "5h", 88.4, 3*time.Hour+2*time.Minute))),
				},
				Vendors: []VendorView{
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
						(&claudecode.Adapter{}).Capabilities()),
					watching(model.VendorCodex, `%USERPROFILE%\.codex`,
						(&codex.Adapter{}).Capabilities()),
				},
			}
			return st
		}},

		// The frame pasted into README.md. Same real capability mix as
		// "v1-capabilities", sized so the row area needs no blank padding.
		{name: "readme", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 9
			st.Snap = Snapshot{
				At: pinned,
				Sessions: []*model.Session{
					sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
						`C:\src\code\telltale`, "claude-opus-5", 12*time.Second,
						withName("telltale")),
					sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000002",
						`C:\src\code\example-app`, "gpt-5.1-codex", 90*time.Second,
						withCtx(189888.0/272000.0*100), derived(),
						withQuota(window("primary", "5h", 88.4, 3*time.Hour+2*time.Minute))),
					sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
						`C:\src\work\acme-api`, "claude-sonnet-4-5", 4*time.Minute,
						withName("acme-api")),
					sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000003",
						`C:\src\code\notes-api`, "gpt-5.1-codex", 22*time.Minute,
						withCtx(12.5), derived()),
				},
				Vendors: []VendorView{
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
						(&claudecode.Adapter{}).Capabilities()),
					watching(model.VendorCodex, `%USERPROFILE%\.codex`,
						(&codex.Adapter{}).Capabilities()),
				},
			}
			return st
		}},

		// Watching and finding nothing is a different fact from a vendor that
		// is not installed. Two words, never a fake row.
		{name: "empty-watching", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 10
			st.Snap = Snapshot{
				At: pinned,
				Vendors: []VendorView{
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
					{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`, Status: StatusNotDetected},
				},
			}
			return st
		}},

		// The third word: the directory exists and the OS refused.
		{name: "empty-unreadable", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 10
			st.Snap = Snapshot{
				At: pinned,
				Vendors: []VendorView{
					{Vendor: model.VendorClaude, Root: `%USERPROFILE%\.claude\projects`,
						Status: StatusUnreadable, Err: "Access is denied."},
					{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`, Status: StatusNotDetected},
				},
			}
			return st
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Render(c.state(), PlainStyles(), GlyphsFor(c.ascii))
			compareGolden(t, c.name, got)
		})
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/hud -update)", err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got != want {
		t.Errorf("render differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// --------------------------------------------------------- unit assertions

// The gauge's whole job is to not lie. This is the §7.4 table, verbatim.
func TestGaugeScale(t *testing.T) {
	g := UnicodeGlyphs()
	sty := PlainStyles()
	cases := []struct {
		pct  *model.Percent
		want string
	}{
		{model.PercentPtr(0), "────────────"},
		{model.PercentPtr(0.4), "▏───────────"},
		{model.PercentPtr(5), "▌───────────"},
		{model.PercentPtr(25), "██▊─────────"},
		{model.PercentPtr(50), "█████▌──────"},
		{model.PercentPtr(84.2), "█████████▎──"},
		{model.PercentPtr(92.6), "██████████▏─"},
		{model.PercentPtr(99.9), "███████████─"},
		{model.PercentPtr(100), "████████████"},
		{nil, "            "},
	}
	for _, c := range cases {
		got := gauge(c.pct, 12, g, sty)
		if got != c.want {
			label := "absent"
			if c.pct != nil {
				label = pctLabel(*c.pct)
			}
			t.Errorf("gauge(%s) = %q, want %q", label, got, c.want)
		}
		if n := len([]rune(got)); n != 12 {
			t.Errorf("gauge width = %d cells, want 12", n)
		}
	}
}

func pctLabel(p model.Percent) string {
	s := &model.Session{ContextPercent: &p}
	return strings.TrimSpace(percentCell(s, PlainStyles(), UnicodeGlyphs()))
}

// Rule 1: only an exact 100% fills the bar. A 99.9% bar rendering solid is a
// gauge claiming "full" when it is not.
func TestGaugeReservesTheLastCellBelow100(t *testing.T) {
	g := UnicodeGlyphs()
	for _, p := range []float64{99, 99.5, 99.9, 99.99} {
		got := gauge(model.PercentPtr(p), 12, g, PlainStyles())
		if !strings.HasSuffix(got, g.Track) {
			t.Errorf("gauge(%v) = %q has no visible track cell", p, got)
		}
	}
	if got := gauge(model.PercentPtr(100), 12, g, PlainStyles()); strings.Contains(got, g.Track) {
		t.Errorf("gauge(100) = %q should be entirely filled", got)
	}
}

// Rule 2: any nonzero value draws at least one eighth.
func TestGaugeNonzeroIsNeverPixelIdenticalToZero(t *testing.T) {
	g := UnicodeGlyphs()
	zero := gauge(model.PercentPtr(0), 12, g, PlainStyles())
	for _, p := range []float64{0.01, 0.1, 0.4} {
		if got := gauge(model.PercentPtr(p), 12, g, PlainStyles()); got == zero {
			t.Errorf("gauge(%v) is pixel-identical to gauge(0): %q", p, got)
		}
	}
}

// Rule 3, and the whole HUD's load-bearing assertion: an absent gauge draws
// nothing at all. An empty track means zero.
func TestAbsentGaugeIsNotAnEmptyTrack(t *testing.T) {
	g := UnicodeGlyphs()
	absent := gauge(nil, 12, g, PlainStyles())
	zero := gauge(model.PercentPtr(0), 12, g, PlainStyles())
	if absent == zero {
		t.Fatal("absent and 0% render identically; the build must fail here")
	}
	if strings.TrimSpace(absent) != "" {
		t.Errorf("absent gauge = %q, want blank", absent)
	}
	if !strings.Contains(zero, g.Track) {
		t.Errorf("0%% gauge = %q, want a full track", zero)
	}
}

// The ASCII set loses partials but keeps every rule.
func TestASCIIGaugeKeepsTheRules(t *testing.T) {
	g := GlyphsFor(true)
	sty := PlainStyles()
	if got := gauge(nil, 12, g, sty); strings.TrimSpace(got) != "" {
		t.Errorf("absent ascii gauge = %q, want blank", got)
	}
	if got := gauge(model.PercentPtr(0), 12, g, sty); got != strings.Repeat("-", 12) {
		t.Errorf("0%% ascii gauge = %q", got)
	}
	if got := gauge(model.PercentPtr(0.4), 12, g, sty); !strings.HasPrefix(got, "#") {
		t.Errorf("0.4%% ascii gauge = %q, want one filled cell", got)
	}
	if got := gauge(model.PercentPtr(99.9), 12, g, sty); !strings.HasSuffix(got, "-") {
		t.Errorf("99.9%% ascii gauge = %q, want a visible track cell", got)
	}
}

// A derived value must be visibly marked as an estimate, never mixed in with
// reported ones (ADR-001).
func TestDerivedValuesCarryTheEstimateMarker(t *testing.T) {
	g := UnicodeGlyphs()
	reported := &model.Session{ContextPercent: model.PercentPtr(69.8)}
	est := &model.Session{
		ContextPercent: model.PercentPtr(69.8),
		Derived:        model.NewFieldSet(model.FieldContextPercent),
	}
	r := percentCell(reported, PlainStyles(), g)
	e := percentCell(est, PlainStyles(), g)
	if r == e {
		t.Fatal("a derived percentage renders identically to a reported one")
	}
	if !strings.Contains(e, "~") {
		t.Errorf("derived cell = %q, want an estimate marker", e)
	}
	if strings.Contains(r, "~") {
		t.Errorf("reported cell = %q must not be marked an estimate", r)
	}
}

// Unknown liveness renders as absent, never as "stale": stale is a claim.
func TestUnknownLivenessRendersBlankNotStale(t *testing.T) {
	st := healthyState(120, 9)
	g := UnicodeGlyphs()
	unknown := sess(model.VendorClaude, "id", `C:\x\y`, "claude-opus-5", 0, noActivity())
	if got := livenessDot(unknown, st, PlainStyles(), g); got != " " {
		t.Errorf("unknown liveness dot = %q, want a blank cell", got)
	}
	stale := sess(model.VendorClaude, "id", `C:\x\y`, "claude-opus-5", 30*time.Minute)
	if got := livenessDot(stale, st, PlainStyles(), g); got != g.DotStale {
		t.Errorf("stale dot = %q, want %q", got, g.DotStale)
	}
}

// Model-authored text can carry U+2028/U+2029 (design.md §4a.2). A session
// name containing one must not tear the grid into two lines.
func TestSessionNameSeparatorsCannotTearTheGrid(t *testing.T) {
	st := healthyState(120, 9)
	st.Snap.Sessions = []*model.Session{
		sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
			`C:\src\code\telltale`, "claude-opus-5", 5*time.Second,
			withName("before\u2028after\u2029end"), withCtx(10), withCost(1)),
	}
	out := Render(st, PlainStyles(), UnicodeGlyphs())
	if strings.ContainsAny(out, "\u2028\u2029") {
		t.Fatal("a separator character reached the rendered frame")
	}
	for i, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > st.Width {
			t.Fatalf("line %d is %d columns wide, budget is %d", i, len([]rune(line)), st.Width)
		}
	}
}

// Every frame must fit the terminal it was asked for, at every tier.
func TestNoLineExceedsTheTerminalWidth(t *testing.T) {
	for _, w := range []int{60, 72, 80, 99, 100, 120, 200} {
		st := healthyState(w, 12)
		for _, help := range []bool{false, true} {
			st.Help = help
			out := Render(st, PlainStyles(), UnicodeGlyphs())
			for i, line := range strings.Split(out, "\n") {
				if n := len([]rune(line)); n > w {
					t.Errorf("width %d, help=%v: line %d is %d columns\n%s", w, help, i, n, line)
				}
			}
		}
	}
}

func TestRenderFillsExactlyTheRequestedHeight(t *testing.T) {
	for _, h := range []int{6, 7, 9, 12, 30} {
		st := healthyState(120, h)
		got := len(strings.Split(Render(st, PlainStyles(), UnicodeGlyphs()), "\n"))
		if got != h {
			t.Errorf("height %d: rendered %d lines", h, got)
		}
	}
}

// A stale scan de-emphasizes the whole row area. Layout goldens render plain,
// so the styling is asserted here.
func TestStaleScanDimsTheRowArea(t *testing.T) {
	fresh := healthyState(120, 9)
	stale := healthyState(120, 9)
	stale.Snap.At = pinned.Add(-47 * time.Second)

	sty := NewStyles(true)
	a := Render(fresh, sty, UnicodeGlyphs())
	b := Render(stale, sty, UnicodeGlyphs())
	if a == b {
		t.Fatal("a 47-second-old scan renders identically to a fresh one")
	}
	if !strings.Contains(b, "\x1b[2m") {
		t.Error("stale render carries no faint attribute")
	}
}

// Mirrors TestThresholdColors in internal/statusline: one escape code per
// severity band, shared through internal/theme.
func TestThresholdColorsMatchTheStatusline(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	cases := []struct {
		pct  float64
		want string
	}{
		{41, "\x1b[32m"},   // green below 60
		{69.8, "\x1b[33m"}, // yellow from 60
		{92.6, "\x1b[31m"}, // red from 85
	}
	for _, c := range cases {
		got := percentCell(&model.Session{ContextPercent: model.PercentPtr(c.pct)}, sty, g)
		if !strings.Contains(got, c.want) {
			t.Errorf("percent %v rendered %q, want the %q band", c.pct, got, c.want)
		}
	}
}

func TestFloorRenders(t *testing.T) {
	st := healthyState(52, 20)
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); got != " telltale needs 60 columns (have 52)" {
		t.Errorf("width floor = %q", got)
	}
	st = healthyState(120, 4)
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); got != " telltale needs 6 rows (have 4)" {
		t.Errorf("height floor = %q", got)
	}
}

func TestDisplayModelNormalizesOnlyWhatItRecognizes(t *testing.T) {
	cases := []struct {
		in   *model.Model
		want string
	}{
		{&model.Model{ID: "claude-opus-5"}, "Opus 5"},
		{&model.Model{ID: "claude-sonnet-4-5"}, "Sonnet 4.5"},
		{&model.Model{ID: "claude-haiku-4-5-20260101"}, "Haiku 4.5"},
		{&model.Model{ID: "claude-opus-5[1m]"}, "Opus 5[1m]"},
		// Unrecognized ids render as themselves rather than as a guess.
		{&model.Model{ID: "gpt-5.1-codex"}, "gpt-5.1-codex"},
		{&model.Model{ID: "something-else"}, "something-else"},
		// A vendor-supplied display name always wins.
		{&model.Model{ID: "claude-opus-5", DisplayName: "Opus 5 (beta)"}, "Opus 5 (beta)"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := DisplayModel(c.in); got != c.want {
			t.Errorf("DisplayModel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSortsAreStableAndPushAbsentValuesDown(t *testing.T) {
	rows := []*model.Session{
		sess(model.VendorClaude, "b", `C:\x\b`, "m", time.Minute),
		sess(model.VendorClaude, "a", `C:\x\a`, "m", time.Minute),
		sess(model.VendorClaude, "c", `C:\x\c`, "m", time.Minute, withCtx(50)),
	}
	sortSessions(rows, SortContext, pinned)
	if rows[0].ID != "c" {
		t.Errorf("context sort put %q first, want the row that has a value", rows[0].ID)
	}
	if rows[1].ID != "a" || rows[2].ID != "b" {
		t.Errorf("equal rows did not fall back to the session key: %q %q", rows[1].ID, rows[2].ID)
	}
}

func TestShowAllRevealsIdleSessionsButNeverHidesUnknownOnes(t *testing.T) {
	st := healthyState(120, 12)
	st.Snap.Sessions = []*model.Session{
		sess(model.VendorClaude, "recent", `C:\x\a`, "m", time.Minute),
		sess(model.VendorClaude, "old", `C:\x\b`, "m", 9*time.Hour),
		// No timestamp is not evidence of age.
		sess(model.VendorClaude, "unknown", `C:\x\c`, "m", 0, noActivity()),
	}
	if got := len(visibleSessions(st)); got != 2 {
		t.Errorf("default view shows %d rows, want 2 (the 9h session is hidden)", got)
	}
	st.ShowAll = true
	if got := len(visibleSessions(st)); got != 3 {
		t.Errorf("show-all shows %d rows, want 3", got)
	}
}

func TestRedactHomeHidesTheUserFromRenderedPaths(t *testing.T) {
	home := `C:\Users\testuser`
	p := filepath.Join(home, ".claude", "projects")
	got := RedactHome(p, home)
	if strings.Contains(got, "testuser") {
		t.Errorf("RedactHome(%q) = %q still names the user", p, got)
	}
	if outside := `C:\opt\shared\.codex`; RedactHome(outside, home) != outside {
		t.Errorf("a path outside home was rewritten: %q", RedactHome(outside, home))
	}
	// Prefix must stop at a path boundary: a sibling user whose name merely
	// starts with the home basename is not inside home.
	if sib := `C:\Users\testuserx\.codex`; RedactHome(sib, home) != sib {
		t.Errorf("sibling dir mangled: %q", RedactHome(sib, home))
	}
	// The exact home directory itself redacts fully.
	if got := RedactHome(home, home); strings.Contains(got, "testuser") {
		t.Errorf("bare home not redacted: %q", got)
	}
	// Empty home disables redaction rather than guessing.
	if got := RedactHome(p, ""); got != p {
		t.Errorf("empty home must disable redaction, got %q", got)
	}
}
