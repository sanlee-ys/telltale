package hud

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	agyadapter "github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	cursoradapter "github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/drift"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/usagecache"
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

// withSubagents sets the fan-out count AND marks it derived, because the
// adapter that produces it does both — a count with no estimate marker would
// be a state model.Validate rejects.
func withSubagents(n int) sessionOpt {
	return func(s *model.Session) {
		s.Subagents = model.Ptr(n)
		s.Derived = s.Derived.With(model.FieldSubagents)
	}
}

func withExtras(kv ...string) sessionOpt {
	return func(s *model.Session) {
		for i := 0; i+1 < len(kv); i += 2 {
			s.Extras = append(s.Extras, model.Extra{Label: kv[i], Value: kv[i+1]})
		}
	}
}

func withDiagnostics(d ...string) sessionOpt {
	return func(s *model.Session) { s.Diagnostics = append(s.Diagnostics, d...) }
}

// codexCanary mirrors internal/adapter/codex's own session_meta canary — the
// first record of every rollout file — including the fields that stop being
// sourceable once it is gone. The literal is written out rather than imported
// because the adapters keep their canaries unexported; what the fixture needs
// is the SHAPE of a real report, and drift.Watch supplies the wording.
var codexCanary = drift.Canary{
	Name: "session_meta",
	Feeds: model.NewFieldSet(
		model.FieldModel, model.FieldWorkspace, model.FieldContextPercent),
}

// withDrift folds a real shape-drift verdict onto the session, through the
// adapter package's own Watch rather than a hand-written diagnostic string.
//
// The HUD recognizes drift by the words drift.Watch produces, so a fixture that
// copied those words by hand would keep passing after the wording moved — while
// the product it is meant to pin went quiet. It must be the LAST option
// applied: Fold reads what the session managed to source, which is the same
// rule the adapters follow.
func withDrift(verified string, cs ...drift.Canary) sessionOpt {
	return func(s *model.Session) { drift.NewWatch(verified, cs...).Fold(s, 1) }
}

// burnSeries builds a pinned sampling history: n samples ending at the pinned
// clock, spanning span, with usage rising linearly from->to.
//
// Golden and unit tests inject the series rather than letting a clock produce
// one, which is the same discipline as State.Now: nothing about the forecast
// depends on how long the test took to run.
func burnSeries(id string, from, to float64, span time.Duration, n int, resets *time.Time) BurnSeries {
	s := BurnSeries{WindowID: id}
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		s.Samples = append(s.Samples, BurnSample{
			At:     pinned.Add(-span).Add(time.Duration(f * float64(span))),
			Used:   from + f*(to-from),
			Resets: resets,
		})
	}
	return s
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

// agySession is an Antigravity CLI row as the real adapter produces one: the
// session's NAME is the head of its conversation id, because the vendor writes
// no human title anywhere a public repo may read — the only free text on its
// disk is prompt content. The model display string is the vendor's own, long
// enough that the 13-column MODEL cell truncates it, which is what the HUD
// really shows and therefore what the golden must pin.
func agySession(age time.Duration) *model.Session {
	return sess(model.VendorAntigravity, "4c8b21a7-0e35-4a12-9f6b-000000000001",
		`C:\src\code\example-app`, "", age,
		withName("4c8b21a7"),
		withModel(&model.Model{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash (High)"}),
		withExtras("uncached in", "40k", "output", "380", "generations", "2"))
}

func withModel(m *model.Model) sessionOpt {
	return func(s *model.Session) { s.Model = m }
}

// cursorSession is a Cursor Composer row as the real adapter produces one, and
// it is the only row in the capability scenes whose CONTEXT cell carries a
// percentage with NO estimate marker: Cursor persists its own
// `contextUsagePercent` and telltale reads it rather than computing one. Next
// to the Codex row's `~69.8%` that contrast is the whole point of the frame.
func cursorSession(age time.Duration) *model.Session {
	return sess(model.VendorCursor, "00000000-eeee-4fff-8aaa-000000000001",
		`C:\src\code\agent-ops`, "", age,
		withName("multi-vendor orchestration"),
		withModel(&model.Model{ID: "composer-2.5", DisplayName: "composer-2.5"}),
		withCtx(37.05234375),
		withExtras("ctx tokens", "94k / 256k"))
}

func watching(v model.VendorID, root string, caps model.Capabilities) VendorView {
	return VendorView{Vendor: v, Root: root, Status: StatusWatching, Caps: caps}
}

// drifted is a vendor view as Scan leaves one whose read reported shape drift.
//
// It runs the SAME roll-up the scan does rather than setting the status and the
// counts by hand, so a golden cannot pin a vendor line the real scan would
// never produce — including the count pair, which is the part a hand-built
// fixture would get subtly wrong first.
func drifted(v model.VendorID, root string, caps model.Capabilities, read []*model.Session) VendorView {
	view := watching(v, root, caps)
	foldDrift(&view, read)
	return view
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

// fleetQuotaState is the header speaking for every vendor it can honestly
// source at once: Codex from its own transcripts (the scan-fresh reading, so
// it alone may forecast), Claude and agy from the statusline relay (§7.15) —
// Claude's reading two hours old and saying so, agy's fresh enough to say
// nothing.
func fleetQuotaState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorCodex, "0f00dbaa-1234-4a77-9b02-000000000042",
				`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute,
				withName("notes-api"),
				withQuota(window("seven_day", "7d", 79, 22*time.Hour+48*time.Minute))),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
		},
		Account: []quotacache.Account{
			{Vendor: model.VendorClaude, WrittenAt: pinned.Add(-2 * time.Hour), Windows: []model.QuotaWindow{
				window("five_hour", "5h", 42, 2*time.Hour+13*time.Minute),
				window("seven_day", "7d", 6, 5*24*time.Hour),
			}},
			{Vendor: model.VendorAntigravity, WrittenAt: pinned.Add(-time.Minute), Windows: []model.QuotaWindow{
				window("gemini-weekly", "gemini-weekly", 38, 3*time.Hour),
			}},
		},
	}
	return st
}

// spendState is the frame §7.16 exists for: one vendor whose account quota is
// readable (Codex, from its own store) and one whose is not and never will be
// without a network call (Cursor) but whose per-turn token counts now arrive
// through the hook relay.
//
// The two facts have to be legible side by side. The quota line speaks for
// Codex and says nothing about Cursor — Cursor's quota stays visibly ABSENT,
// which is the honest state — while the spend line says what Cursor's turns
// actually cost this machine. Neither may be mistaken for the other, which is
// what TestSpendIsNeverRenderedAsQuota asserts on this exact render.
func spendState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorCodex, "0f00dbaa-1234-4a77-9b02-000000000042",
				`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute,
				withName("notes-api"),
				withQuota(window("seven_day", "7d", 79, 22*time.Hour+48*time.Minute))),
			sess(model.VendorCursor, "3c7f0a11-5566-4d88-9e00-000000000007",
				`C:\src\code\telltale`, "composer-2.5", 90*time.Second,
				withName("Multi-vendor orchestration"), withCtx(37)),
		},
		Vendors: []VendorView{
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
			watching(model.VendorCursor, `%APPDATA%\Cursor\User`, cursoradapter.New().Capabilities()),
		},
		Spend: []usagecache.Total{{
			Vendor: model.VendorCursor,
			Entry: usagecache.Entry{
				Vendor:           string(model.VendorCursor),
				Since:            pinned.Add(-12 * time.Minute),
				WrittenAt:        pinned.Add(-90 * time.Second),
				Turns:            14,
				InputTokens:      48012,
				OutputTokens:     1203,
				CacheReadTokens:  1904221,
				CacheWriteTokens: 62004,
			},
		}},
	}
	return st
}

// usageFleetState is the frame §7.17 exists for: every source state telltale
// can be in, at once, so the view has to keep them apart in one screen.
//
//   - CODEX — quota from its own store, re-measured this scan.
//   - CLAUDE — quota relayed by the statusline two hours ago, and saying so.
//   - AGY — quota relayed a minute ago, fresh enough to say nothing about age.
//   - CURSOR — no quota anywhere and never will be without a network call, but
//     a token total from the hook relay. Spend with no quota beside it.
//   - GEMINI — sessions on this machine and nothing to say about either: the
//     absence line, with its reason.
//
// A sixth vendor is deliberately NOT here: one with no sessions, no quota and
// no total does not appear at all, which is what TestUsageOmitsAVendorItHasNothingToSayAbout
// pins.
func usageFleetState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorCodex, "0f00dbaa-1234-4a77-9b02-000000000042",
				`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute,
				withName("notes-api"),
				withQuota(window("seven_day", "7d", 79, 22*time.Hour+48*time.Minute))),
			sess(model.VendorGemini, "session-2026-08-02T09-58-0a1b2c3d",
				`c:\src\code\learning-notes`, "gemini-3-pro", 3*time.Minute,
				withName("glossary tooltips")),
			cursorSession(70 * time.Second),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
				(&claudecode.Adapter{}).Capabilities()),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
			watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`,
				(&gemini.Adapter{}).Capabilities()),
			watching(model.VendorCursor, `%APPDATA%\Cursor\User`,
				(&cursoradapter.Adapter{}).Capabilities()),
		},
		Account: []quotacache.Account{
			{Vendor: model.VendorClaude, WrittenAt: pinned.Add(-2 * time.Hour), Windows: []model.QuotaWindow{
				window("five_hour", "5h", 42, 2*time.Hour+13*time.Minute),
				window("seven_day", "7d", 6, 5*24*time.Hour),
			}},
			{Vendor: model.VendorAntigravity, WrittenAt: pinned.Add(-time.Minute), Windows: []model.QuotaWindow{
				window("gemini-weekly", "gemini-weekly", 38, 3*time.Hour),
			}},
		},
		Spend: []usagecache.Total{{
			Vendor: model.VendorCursor,
			Entry: usagecache.Entry{
				Vendor:           string(model.VendorCursor),
				Since:            pinned.Add(-12 * time.Minute),
				WrittenAt:        pinned.Add(-90 * time.Second),
				Turns:            14,
				InputTokens:      48012,
				OutputTokens:     1203,
				CacheReadTokens:  1904221,
				CacheWriteTokens: 62004,
			},
		}},
	}
	st.Usage = true
	return st
}

// usageEmptyState is the same view on a machine that has told telltale nothing:
// five vendors watched, no session, no relay entry, no total. The body says so
// in a sentence rather than drawing five blocks of dashes — a table of nothing
// is a table asserting it measured five things.
func usageEmptyState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Vendors: []VendorView{
			{Vendor: model.VendorAntigravity, Root: `%USERPROFILE%\.gemini\antigravity-cli`,
				Status: StatusNotDetected},
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
			{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`, Status: StatusNotDetected},
			{Vendor: model.VendorCursor, Root: `%APPDATA%\Cursor\User`, Status: StatusNotDetected},
			{Vendor: model.VendorGemini, Root: `%USERPROFILE%\.gemini\tmp`, Status: StatusNotDetected},
		},
	}
	st.Usage = true
	return st
}

// v11State is the v1.1 fixture set, rendered with the REAL adapter capability
// tables rather than the synthetic everything-vendor. Three Claude sessions
// (one fanning out two sub-agents, one that measured zero, one running five)
// and one Codex session whose records did not parse.
func v11State(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
				`C:\src\code\telltale`, "claude-opus-5", 12*time.Second,
				withName("telltale"), withSubagents(2),
				withExtras("branch", "main", "cli", "2.1.219", "ctx tokens", "215k")),
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
				`C:\src\work\acme-api`, "claude-sonnet-4-5", 48*time.Second,
				withName("acme-api"), withSubagents(0),
				withExtras("branch", "release", "cli", "2.1.219", "ctx tokens", "88k")),
			// Discovered by filename; every record torn, so nothing parsed.
			sess(model.VendorCodex, "4f2a9c81-1d3e-4a77-9b02-000000000000",
				"", "", 7*time.Minute,
				degraded(model.FieldWorkspace), degraded(model.FieldContextPercent),
				withDiagnostics("2 unparseable records skipped", "no turn_context record in the read window")),
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000003",
				`C:\src\code\learning-notes`, "claude-haiku-4-5", 22*time.Minute,
				withName("learning-notes"), withSubagents(5),
				withExtras("branch", "main", "cli", "2.1.219", "ctx tokens", "31k")),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
				(&claudecode.Adapter{}).Capabilities()),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`,
				(&codex.Adapter{}).Capabilities()),
		},
	}
	return st
}

// ------------------------------------------------------------------ goldens

type goldenCase struct {
	name  string
	state func() State
	ascii bool
}

func goldenCases() []goldenCase {
	return []goldenCase{
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

		// 17 rows, not 16: the overlay gained the `u` key and at 16 the body
		// clipped its own last line — "? close this help" — which is the one
		// line a reader stuck in the overlay most needs. The overlay scrolls,
		// so this was never a correctness bug; it was a frame the design doc
		// pastes showing the product hiding its exit.
		{name: "help", state: func() State {
			st := healthyState(120, 17)
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
		// Codex sources a derived context percentage and real quota windows;
		// Gemini sources name/model/workspace and a derived sub-agent count,
		// with no quota, context or cost anywhere on its disk seam; Antigravity
		// sources the same four fields as Gemini minus the sub-agent count,
		// labelled by its conversation id because the only free text on its
		// disk is somebody's prompt; and Cursor sources a context percentage the
		// vendor itself wrote down, which is why its CONTEXT cell carries a bar
		// and no estimate marker beside the Codex row's computed one.
		{name: "v1-capabilities", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 10
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
					// The registry records the project path lowercased on
					// Windows — the fixture keeps that fidelity.
					sess(model.VendorGemini, "session-2026-08-02T09-58-0a1b2c3d",
						`c:\src\code\learning-notes`, "gemini-3-pro", 3*time.Minute,
						withName("glossary tooltips"), withSubagents(2),
						withExtras("ctx tokens", "215k")),
					agySession(2 * time.Minute),
					cursorSession(70 * time.Second),
				},
				Vendors: []VendorView{
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
						(&claudecode.Adapter{}).Capabilities()),
					watching(model.VendorCodex, `%USERPROFILE%\.codex`,
						(&codex.Adapter{}).Capabilities()),
					watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`,
						(&gemini.Adapter{}).Capabilities()),
					watching(model.VendorAntigravity, `%USERPROFILE%\.gemini\antigravity-cli`,
						(&agyadapter.Adapter{}).Capabilities()),
					watching(model.VendorCursor, `%APPDATA%\Cursor\User`,
						(&cursoradapter.Adapter{}).Capabilities()),
				},
			}
			return st
		}},

		// The frame pasted into README.md. Same real capability mix as
		// "v1-capabilities", sized so every row is visible (one pad line —
		// the row area's slot math can't land on exactly five, and every new
		// vendor adds a row: five at v1, six with agy, seven with Cursor).
		{name: "readme", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 13
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
					sess(model.VendorGemini, "session-2026-08-02T09-58-0a1b2c3d",
						`c:\src\code\learning-notes`, "gemini-3-pro", 3*time.Minute,
						withName("glossary tooltips"), withSubagents(2),
						withExtras("ctx tokens", "215k")),
					agySession(2 * time.Minute),
					cursorSession(70 * time.Second),
				},
				Vendors: []VendorView{
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
						(&claudecode.Adapter{}).Capabilities()),
					watching(model.VendorCodex, `%USERPROFILE%\.codex`,
						(&codex.Adapter{}).Capabilities()),
					watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`,
						(&gemini.Adapter{}).Capabilities()),
					watching(model.VendorAntigravity, `%USERPROFILE%\.gemini\antigravity-cli`,
						(&agyadapter.Adapter{}).Capabilities()),
					watching(model.VendorCursor, `%APPDATA%\Cursor\User`,
						(&cursoradapter.Adapter{}).Capabilities()),
				},
			}
			return st
		}},

		// Watching and finding nothing is a different fact from a vendor that
		// is not installed. Two words, never a fake row.
		{name: "empty-watching", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 11
			st.Snap = Snapshot{
				At: pinned,
				Vendors: []VendorView{
					// Scan sorts the vendor views by id, so agy leads.
					{Vendor: model.VendorAntigravity, Root: `%USERPROFILE%\.gemini\antigravity-cli`,
						Status: StatusNotDetected},
					watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
					{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`, Status: StatusNotDetected},
					{Vendor: model.VendorCursor, Root: `%APPDATA%\Cursor\User`, Status: StatusNotDetected},
					{Vendor: model.VendorGemini, Root: `%USERPROFILE%\.gemini\tmp`, Status: StatusNotDetected},
				},
			}
			return st
		}},

		// ---------------------------------------------------------- v1.1

		// The detail pane over a Claude row, with the REAL adapter capability
		// table: the honesty machinery on screen. "not sourced" names the
		// three fields Claude cannot put on disk, which is why the row's
		// CONTEXT and COST cells were empty in the first place.
		{name: "detail-pane", state: func() State {
			st := v11State(120, 18)
			st.Detail = true
			st.Cursor = 0
			return st
		}},

		// The same pane over a session whose records did not parse. Degraded
		// field names and the adapter's own diagnostics, which v1 carried and
		// never displayed.
		{name: "detail-degraded", state: func() State {
			st := v11State(120, 17)
			st.Detail = true
			st.Cursor = 2
			return st
		}},

		// The v1.1 row grammar, both changes at once: the selection mark in
		// the leading pad column, and the fan-out chip. Row 2 measured ZERO
		// sub-agents and therefore draws no chip at all — a "⑂0" would be a
		// claim nobody asked for.
		{name: "row-grammar", state: func() State {
			st := v11State(120, 9)
			st.Cursor = 0
			return st
		}},

		// The burn forecast. The 5h window has 7 samples over 18 minutes and a
		// projection that lands before its own reset; the 7d window has the
		// same basis and is moving too slowly to reach 100% inside a day, so
		// it renders NOTHING rather than a wild line.
		{name: "burn-forecast", state: func() State {
			st := healthyState(120, 10)
			resets5 := pinned.Add(2*time.Hour + 13*time.Minute)
			resets7 := pinned.Add(5*24*time.Hour + 2*time.Hour)
			st.Burn = Burn{Series: []BurnSeries{
				burnSeries("five_hour", 30, 42, 18*time.Minute, 7, &resets5),
				burnSeries("seven_day", 18, 18.1, 18*time.Minute, 7, &resets7),
			}}
			return st
		}},

		// Every vendor the header can honestly speak for, at once: Codex
		// transcript-sourced, Claude and agy relayed (§7.15). At 120 columns
		// three vendors and four windows shed the gauges — the cascade keeps
		// every fact (vendor, window, reading, reset, and the stale reading's
		// age) and spends the bars first.
		{name: "quota-fleet", state: func() State {
			return fleetQuotaState(120, 9)
		}},

		// The token relay (§7.16). Codex's quota is on the quota line; Cursor
		// has none there and is not faked into it, and its spend gets a line
		// of its own with a verb on it. No percentage, no bar, no ceiling —
		// there is no denominator anywhere in this reading.
		{name: "spend-cursor", state: func() State {
			return spendState(120, 10)
		}},

		// ------------------------------------------------- the usage view

		// Every source state at once (§7.17): scan-fresh quota, relayed quota
		// carrying its age, relayed quota fresh enough not to, spend with no
		// quota beside it, and a vendor with neither saying which kind of
		// nothing that is.
		{name: "usage-fleet", state: func() State { return usageFleetState(120, 22) }},

		// The same view at the 60-column floor. The gauges are gone — the grid's
		// own shed order, because the bar re-states a number that is still on
		// screen — and every fact survives, including the relayed reading's age
		// and the spend total's window.
		{name: "usage-floor", state: func() State { return usageFleetState(60, 22) }},

		// ASCII. The distinction between a reading against a limit and a count
		// with no limit is carried by words and by which vocabulary each line
		// uses, so nothing about it depends on the Unicode set.
		{name: "usage-ascii", ascii: true, state: func() State { return usageFleetState(120, 22) }},

		// Nothing measured anywhere: a sentence, not a table of dashes.
		{name: "usage-empty", state: func() State { return usageEmptyState(120, 10) }},

		// Find mode: the footer becomes the query line and says how to leave.
		{name: "find-active", state: func() State {
			st := healthyState(120, 9)
			st.Finding = true
			st.Query = "api"
			return st
		}},

		// The query applied with the mode left. The header count reads "2 of
		// 4" and the footer keeps naming the query, because a filter the user
		// has forgotten about hides rows just as silently as one they cannot
		// see.
		{name: "find-applied", state: func() State {
			st := healthyState(120, 9)
			st.Query = "api"
			return st
		}},

		// ----------------------------------------------------- shape drift

		// The fourth word, in the state it actually happens in. Every row here
		// is identical to "wide-healthy" except the Codex one, whose read found
		// no session_meta record — so everything that record feeds is absent
		// and the row renders EXACTLY as it would if Codex simply had nothing
		// to say. That is the failure: nothing in the grid can tell those two
		// apart, and the footer notice is the only thing on screen that knows.
		{name: "shape-drift", state: func() State {
			st := healthyState(120, 9)
			rows := healthy()
			rows[2] = sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000001",
				"", "", 4*time.Minute,
				withDrift("codex-cli 0.146.0", codexCanary))
			st.Snap.Sessions = rows
			st.Snap.Vendors = []VendorView{
				watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
				drifted(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps, rows[2:3]),
			}
			return st
		}},

		// The fourth word ON the vendor line, which needs the narrow state
		// where sessions exist and no row is visible — here because both Codex
		// sessions are past the idle cutoff. One of the two drifted, and the
		// line says which of the two kinds of "any" that was: a store mid-
		// rollout, not a format that moved under all of it.
		{name: "empty-drifted", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 11
			read := []*model.Session{
				sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000001",
					`C:\src\code\notes-api`, "gpt-5.1-codex", 9*time.Hour),
				sess(model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000002",
					"", "", 11*time.Hour,
					withDrift("codex-cli 0.146.0", codexCanary)),
			}
			st.Snap = Snapshot{
				At:       pinned,
				Sessions: read,
				Vendors: []VendorView{
					{Vendor: model.VendorAntigravity, Root: `%USERPROFILE%\.gemini\antigravity-cli`,
						Status: StatusNotDetected},
					{Vendor: model.VendorClaude, Root: `%USERPROFILE%\.claude\projects`,
						Status: StatusNotDetected},
					drifted(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps, read),
					{Vendor: model.VendorCursor, Root: `%APPDATA%\Cursor\User`, Status: StatusNotDetected},
					{Vendor: model.VendorGemini, Root: `%USERPROFILE%\.gemini\tmp`, Status: StatusNotDetected},
				},
			}
			return st
		}},

		// The third word: the directory exists and the OS refused.
		{name: "empty-unreadable", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 11
			st.Snap = Snapshot{
				At: pinned,
				Vendors: []VendorView{
					{Vendor: model.VendorAntigravity, Root: `%USERPROFILE%\.gemini\antigravity-cli`,
						Status: StatusNotDetected},
					{Vendor: model.VendorClaude, Root: `%USERPROFILE%\.claude\projects`,
						Status: StatusUnreadable, Err: "Access is denied."},
					{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`, Status: StatusNotDetected},
					{Vendor: model.VendorCursor, Root: `%APPDATA%\Cursor\User`, Status: StatusNotDetected},
					{Vendor: model.VendorGemini, Root: `%USERPROFILE%\.gemini\tmp`, Status: StatusNotDetected},
				},
			}
			return st
		}},
	}
}

func TestGoldenRenders(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			got := Render(c.state(), PlainStyles(), GlyphsFor(c.ascii))
			compareGolden(t, c.name, got)
		})
	}
}

// Every fixture that feeds a golden must be a session an adapter could
// legally have produced. Without this the goldens are free to pin a render of
// a state the schema forbids — a gauge drawn from data no honest adapter can
// emit, which is exactly the thing the harness exists to catch.
func TestGoldenFixturesSatisfyValidate(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			st := c.state()
			caps := map[model.VendorID]model.Capabilities{}
			for _, v := range st.Snap.Vendors {
				caps[v.Vendor] = v.Caps
			}
			for _, s := range st.Snap.Sessions {
				if err := s.Validate(caps[s.Vendor]); err != nil {
					t.Errorf("session %s: %v", s.Key(), err)
				}
			}
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

// ------------------------------------------------ the token relay (§7.16)

// spendLineOf returns the header line carrying the spend block, or "" — found
// by the verb, which is the thing the design says must always be on it.
func spendLineOf(render string) string {
	for _, line := range strings.Split(render, "\n") {
		if strings.Contains(line, "spent") {
			return line
		}
	}
	return ""
}

// The one regression this feature can cause. Tokens spent have NO denominator
// — Cursor publishes no account limit without a network call (§3.9) — so the
// spend line may never borrow the quota line's vocabulary. A percentage or a
// bar here would invent a ceiling out of nothing, which is the same class of
// error as filling a CapNone field with a plausible guess.
func TestSpendIsNeverRenderedAsQuota(t *testing.T) {
	g := UnicodeGlyphs()
	got := Render(spendState(120, 10), PlainStyles(), g)
	line := spendLineOf(got)
	if line == "" {
		t.Fatal("no spend line in the render")
	}
	if strings.Contains(line, "%") {
		t.Errorf("the spend line rendered a percentage — of what?\n%s", line)
	}
	for _, bar := range append([]string{g.Fill, g.Track}, g.Eighths...) {
		if strings.Contains(line, bar) {
			t.Errorf("the spend line drew a gauge glyph %q — a bar implies a ceiling\n%s", bar, line)
		}
	}
	if strings.Contains(line, g.Reset) {
		t.Errorf("the spend line drew a reset countdown; a counter does not reset\n%s", line)
	}
	// And the other half of the same claim: Cursor's quota stays ABSENT. The
	// quota block is asserted directly rather than by scanning the frame,
	// because the identity line legitimately says "cursor 1" — that is a
	// session count, and the distinction between it and a quota reading is
	// exactly what this test would otherwise blur.
	st := spendState(120, 10)
	quota := quotaBlock(st, PlainStyles(), g, st.Width)
	if strings.Contains(strings.ToLower(quota), "cursor") || strings.Contains(quota, "cu ") {
		t.Errorf("cursor was given a quota block it has no source for:\n%s", quota)
	}
	if !strings.Contains(quota, "codex") {
		t.Errorf("the vendor that DOES have quota lost its block:\n%s", quota)
	}
}

// The window is what makes a sum honest, so it never sheds — at any width, in
// any dress. "48k" alone is a number pretending to be a state.
func TestSpendAlwaysCarriesItsWindow(t *testing.T) {
	for _, w := range []int{200, 150, 120, 100, 90, 80, 70, 60} {
		got := Render(spendState(w, 12), PlainStyles(), UnicodeGlyphs())
		line := spendLineOf(got)
		if line == "" {
			t.Errorf("width %d: the spend line vanished entirely", w)
			continue
		}
		if !strings.Contains(line, "over 10m") {
			t.Errorf("width %d: the accumulation window shed:\n%s", w, line)
		}
		// in/out are fact and shed with it.
		if !strings.Contains(line, "in 48k") || !strings.Contains(line, "out 1.2k") {
			t.Errorf("width %d: a token count shed:\n%s", w, line)
		}
	}
}

// Dropping is never silent (the footer's rule). When the line cannot fit the
// cache pair, it says so with the ellipsis rather than quietly showing two
// numbers where there were four.
func TestSpendSaysWhenItDroppedTheCachePair(t *testing.T) {
	g := UnicodeGlyphs()
	wide := spendLineOf(Render(spendState(120, 10), PlainStyles(), g))
	if !strings.Contains(wide, "cache read") || strings.Contains(wide, g.Ellipsis) {
		t.Errorf("at 120 the full dress should fit undecorated:\n%s", wide)
	}
	narrow := spendLineOf(Render(spendState(70, 12), PlainStyles(), g))
	if strings.Contains(narrow, "cache") {
		t.Errorf("at 70 the cache pair should have shed:\n%s", narrow)
	}
	if !strings.Contains(narrow, g.Ellipsis) {
		t.Errorf("the cache pair shed without saying so:\n%s", narrow)
	}
}

// Past the age threshold the reading carries its own age — the §7.12 basis
// rule the relayed quota block follows, applied to a total. Without it a
// morning's spend reads as this minute's.
func TestAStaleSpendTotalCarriesItsAge(t *testing.T) {
	st := spendState(120, 10)
	st.Snap.Spend[0].WrittenAt = pinned.Add(-2 * time.Hour)
	line := spendLineOf(Render(st, PlainStyles(), UnicodeGlyphs()))
	if !strings.Contains(line, "2h ago") {
		t.Errorf("a two-hour-old total rendered without its age:\n%s", line)
	}

	fresh := spendLineOf(Render(spendState(120, 10), PlainStyles(), UnicodeGlyphs()))
	if strings.Contains(fresh, "ago") {
		t.Errorf("a 90-second-old total wore an age it did not need:\n%s", fresh)
	}
}

// Absence is not zero (§4a.1), here as everywhere. A machine that has never
// fired the hook has no spend line at all — not a line of zeros, which would
// assert that nothing was spent rather than that nothing was measured.
func TestNoRelayedTotalRendersNoLine(t *testing.T) {
	st := spendState(120, 10)
	st.Snap.Spend = nil
	got := Render(st, PlainStyles(), UnicodeGlyphs())
	if line := spendLineOf(got); line != "" {
		t.Errorf("a vendor with no relayed total rendered a line:\n%s", line)
	}
	if strings.Contains(got, " 0 ") && strings.Contains(got, "spent") {
		t.Errorf("absence rendered as a zeroed total:\n%s", got)
	}
}

// The spend line never shares a row with quota, at any width. They carry
// different kinds of number and a reader scanning one line of vendor blocks
// would take the second for more of the first.
func TestSpendNeverSharesALineWithQuota(t *testing.T) {
	for _, w := range []int{200, 160, 120, 100, 80, 60} {
		got := Render(spendState(w, 12), PlainStyles(), UnicodeGlyphs())
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "spent") && strings.Contains(line, "%") {
				t.Errorf("width %d: quota and spend shared a line:\n%s", w, line)
			}
		}
	}
}

// The ASCII set has to carry the same distinction. The verb is a word, not a
// glyph, precisely so --ascii and NO_COLOR lose nothing of the claim.
func TestSpendSurvivesASCII(t *testing.T) {
	got := Render(spendState(120, 10), PlainStyles(), GlyphsFor(true))
	line := spendLineOf(got)
	if line == "" {
		t.Fatal("the spend line vanished in ASCII")
	}
	if !strings.Contains(line, "cursor spent") || !strings.Contains(line, "over 10m") {
		t.Errorf("ASCII lost part of the claim:\n%s", line)
	}
	if strings.Contains(line, "%") || strings.Contains(line, "#") {
		t.Errorf("ASCII rendered a gauge or a percentage:\n%s", line)
	}
}

// ------------------------------------------- the fleet usage view (§7.17)

// usageBodyOf returns the view's body: everything between the two rules. The
// header is deliberately excluded, because the header carries its OWN quota and
// spend blocks and several assertions below would otherwise pass on the
// header's evidence while the body did whatever it liked.
func usageBodyOf(t *testing.T, st State, g Glyphs) []string {
	t.Helper()
	lines := strings.Split(Render(st, PlainStyles(), g), "\n")
	// Matched against the rule the renderer itself draws, not against "a run of
	// track glyphs": the view's own gauges are made of the same character, and
	// a 20-cell empty track is a perfectly good imitation of a horizontal rule.
	sep := rule(st.Width, PlainStyles(), g)
	var body []string
	rules := 0
	for _, l := range lines {
		if l == sep {
			rules++
			continue
		}
		if rules == 1 {
			body = append(body, l)
		}
	}
	if rules != 2 {
		t.Fatalf("expected a body between two rules, found %d rules", rules)
	}
	return body
}

// The load-bearing assertion of this surface, and §7.1 principle 1 applied to
// it: a vendor with no quota reading never renders a number. "0%" would say the
// account has used none of its allowance, which is a measurement telltale did
// not make and, for Gemini and Cursor, one that does not exist to be made.
func TestUsageNeverRendersAQuotaLessVendorAsZero(t *testing.T) {
	g := UnicodeGlyphs()
	for _, line := range usageBodyOf(t, usageFleetState(120, 22), g) {
		for _, v := range []string{" gemini", " cursor"} {
			if strings.HasPrefix(line, v) && strings.Contains(line, "%") {
				t.Errorf("a vendor with no quota source rendered a percentage:\n%s", line)
			}
		}
	}
	// And the general form: no line anywhere in the body pairs a bar with a
	// zero it never read.
	for _, line := range usageBodyOf(t, usageEmptyState(120, 10), g) {
		if strings.Contains(line, "%") || strings.Contains(line, g.Fill) {
			t.Errorf("a machine with no readings drew a gauge or a percentage:\n%s", line)
		}
	}
}

// TestSpendIsNeverRenderedAsQuota's claim, extended to the surface that puts
// the two measurements four lines apart instead of on separate header rows.
// Proximity is exactly what makes this the riskier render of the two.
func TestUsageSpendBorrowsNoneOfQuotasVocabulary(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		var spent string
		for _, line := range usageBodyOf(t, usageFleetState(120, 22), g) {
			if strings.Contains(line, "spent") {
				spent = line
			}
		}
		if spent == "" {
			t.Fatalf("ascii=%v: no spend line in the usage body", ascii)
		}
		if strings.Contains(spent, "%") {
			t.Errorf("ascii=%v: the spend line rendered a percentage — of what?\n%s", ascii, spent)
		}
		for _, bar := range append([]string{g.Fill}, g.Eighths...) {
			if strings.Contains(spent, bar) {
				t.Errorf("ascii=%v: the spend line drew %q — a bar implies a ceiling\n%s", ascii, bar, spent)
			}
		}
		if strings.Contains(spent, g.Reset) {
			t.Errorf("ascii=%v: the spend line drew a reset countdown; a counter does not reset\n%s", ascii, spent)
		}
		if !strings.Contains(spent, "spent") {
			t.Errorf("ascii=%v: the spend line lost its verb\n%s", ascii, spent)
		}
	}
}

// The sum is only honest while it says what it is a sum OF, at every width and
// in every dress (§7.16). A bare token count is a number pretending to be a
// state.
func TestUsageTotalNeverPrintsWithoutItsWindow(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 72, 60} {
		var spent string
		for _, line := range usageBodyOf(t, usageFleetState(w, 22), UnicodeGlyphs()) {
			if strings.Contains(line, "spent") {
				spent = line
			}
		}
		if spent == "" {
			t.Errorf("width %d: the spend line vanished entirely", w)
			continue
		}
		if !strings.Contains(spent, "over 10m") {
			t.Errorf("width %d: the accumulation window shed:\n%s", w, spent)
		}
		if !strings.Contains(spent, "in 48k") || !strings.Contains(spent, "out 1.2k") {
			t.Errorf("width %d: a token count shed:\n%s", w, spent)
		}
	}
}

// §4a.1 on this surface: the three ways a vendor can have no quota are three
// different facts, and a reader deciding whether to go and DO something needs
// them apart. "The statusline writes it" is an action; "its store holds
// experiment values" is a closed door.
func TestUsageKeepsTheKindsOfAbsenceApart(t *testing.T) {
	body := strings.Join(usageBodyOf(t, usageFleetState(120, 22), UnicodeGlyphs()), "\n")
	for _, want := range []string{
		// structurally absent, with the measurement behind the verdict.
		"its store holds experiment values, not usage",
		// structurally absent, the other vendor, in its own words.
		"no quota reaches disk anywhere telltale can read",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the usage body never says %q:\n%s", want, body)
		}
	}

	// The third kind — the seam exists and has never fired — cannot appear in
	// the same frame as the other two: Claude and agy are the only vendors
	// whose quota travels by relay, and the fleet golden spends both of them on
	// readings that DID arrive. So it is constructed here: a Claude session on
	// the machine, and no relay entry for it.
	//
	// This line names the statusline, and that naming is the point. It is the
	// one absence on this surface a user can act on, and an absence with an
	// action behind it that does not say the action is just a shrug.
	seam := usageFleetState(120, 22)
	seam.Snap.Account = seam.Snap.Account[1:] // agy keeps its reading; claude loses one
	seam.Snap.Sessions = append(seam.Snap.Sessions,
		sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000009",
			`C:\src\code\telltale`, "claude-opus-5", 12*time.Second, withName("telltale")))
	got := strings.Join(usageBodyOf(t, seam, UnicodeGlyphs()), "\n")
	if !strings.Contains(got, "no quota relayed yet · the telltale statusline writes it") {
		t.Errorf("a vendor whose relay has never fired did not name the statusline:\n%s", got)
	}
	// And it must not be confusable with the structural case: a seam that has
	// never fired is not a seam that does not exist.
	for _, never := range []string{"anywhere", "reaches disk"} {
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, " claude") && strings.Contains(line, never) {
				t.Errorf("a never-fired relay was described as structurally absent:\n%s", line)
			}
		}
	}
	// A relayed reading that DID arrive says so, and says how old it is —
	// §7.15's age rule, which is what keeps "aged out" from being mistaken for
	// "fresh" in the one direction the reader could be misled.
	if !strings.Contains(body, "relayed by the statusline · 2h ago") {
		t.Errorf("a two-hour-old relayed reading rendered without its age:\n%s", body)
	}
	// And the scan-fresh block is not described as relayed. §7.15 makes the
	// distinction load-bearing: only one of the two may ever carry a forecast.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, " codex") && strings.Contains(line, "relayed") {
			t.Errorf("a transcript-sourced block claimed to be relayed:\n%s", line)
		}
	}
}

// The view is not a checklist of every adapter that was compiled in. A vendor
// with no sessions, no quota and no total is not on this machine in any sense
// telltale measured, and a block of dashes for it would assert otherwise.
func TestUsageOmitsAVendorItHasNothingToSayAbout(t *testing.T) {
	st := usageFleetState(120, 22)
	body := strings.Join(usageBodyOf(t, st, UnicodeGlyphs()), "\n")
	// agy has a relayed reading and no sessions: it appears.
	if !strings.Contains(body, " agy") {
		t.Errorf("a vendor with a relayed reading and no sessions was dropped:\n%s", body)
	}
	// Now take both relayed readings away. Neither vendor has a session on this
	// machine, so neither has anything telltale measured — and an absence line
	// is for a vendor that is HERE and silent, not for one that is not here at
	// all. Both must vanish outright.
	st.Snap.Account = nil
	body = strings.Join(usageBodyOf(t, st, UnicodeGlyphs()), "\n")
	for _, gone := range []string{" agy", " claude"} {
		if strings.Contains(body, gone) {
			t.Errorf("a vendor with no sessions and no reading still got a block (%s):\n%s", gone, body)
		}
	}
	// The vendors that ARE here are untouched by that.
	for _, kept := range []string{" codex", " gemini", " cursor"} {
		if !strings.Contains(body, kept) {
			t.Errorf("a vendor with sessions lost its block (%s):\n%s", kept, body)
		}
	}
}

// Fixed fleet order, never sorted by usage. Position is the navigation, so a
// vendor moving must mean a vendor was added or removed — not that another
// vendor's percentage crossed it.
func TestUsageOrderIsTheFleetOrderNotTheReadings(t *testing.T) {
	st := usageFleetState(120, 22)
	want := []string{" claude", " codex", " gemini", " agy", " cursor"}

	check := func(label string) {
		t.Helper()
		var got []string
		for _, line := range usageBodyOf(t, st, UnicodeGlyphs()) {
			for _, v := range want {
				if strings.HasPrefix(line, v+" ") || line == v {
					got = append(got, v)
				}
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: block order %v, want %v", label, got, want)
		}
	}
	check("as measured")

	// Invert every reading. The heaviest user is now the lightest and nothing
	// may move.
	for i := range st.Snap.Account {
		for j := range st.Snap.Account[i].Windows {
			p := 100 - float64(*st.Snap.Account[i].Windows[j].UsedPercent)
			st.Snap.Account[i].Windows[j].UsedPercent = model.PercentPtr(p)
		}
	}
	check("readings inverted")
}

// The gauge is the reason this surface exists rather than a wider header: one
// window per line buys the bar real room. 20 cells at the wide tier against the
// header's 8, shedding on the grid's own breakpoints rather than on new ones.
func TestUsageGivesTheGaugeRealRoom(t *testing.T) {
	g := UnicodeGlyphs()
	for _, c := range []struct {
		width, cells int
	}{{120, usageGaugeWide}, {100, usageGaugeWide}, {99, usageGaugeCompact}, {80, usageGaugeCompact}, {79, 0}, {60, 0}} {
		if got := usageGaugeFor(c.width); got != c.cells {
			t.Errorf("width %d: gauge is %d cells, want %d", c.width, got, c.cells)
		}
	}
	if usageGaugeWide <= quotaGauge {
		t.Errorf("the usage view's bar (%d) is no wider than the header's cramped one (%d)",
			usageGaugeWide, quotaGauge)
	}
	// The bar sheds and the number it encodes does not — the grid's rule
	// (§7.2), and the reason shedding it is allowed at all.
	narrow := strings.Join(usageBodyOf(t, usageFleetState(60, 22), g), "\n")
	if strings.Contains(narrow, g.Fill) {
		t.Errorf("the bar survived the narrow tier:\n%s", narrow)
	}
	for _, want := range []string{"42%", "79%", "↻ 2h13m", "2h ago"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the narrow tier shed the fact %q, not just the decoration:\n%s", want, narrow)
		}
	}
}

// §7.3: the drift notice renders under EVERY body, and a third body is a third
// chance to lose it. A warning that comes and goes depending on which pane is
// open is one a reader cannot trust to be there.
func TestTheDriftNoticeRendersUnderTheUsageView(t *testing.T) {
	st := driftState(120, 14, model.VendorCodex)
	st.Usage = true
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); !strings.Contains(got, "codex drifted") {
		t.Errorf("usage body: the drift notice is missing\n%s", got)
	}
}

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
		// The usage view is a third body with its own line budget — its
		// headings carry sentences rather than cells, which is exactly the
		// shape that overruns a narrow frame.
		out := Render(usageFleetState(w, 22), PlainStyles(), UnicodeGlyphs())
		for i, line := range strings.Split(out, "\n") {
			if n := len([]rune(line)); n > w {
				t.Errorf("width %d, usage: line %d is %d columns\n%s", w, i, n, line)
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
		// Families are not allowlisted: the next family must not render as a
		// truncated raw id (claude-fable-5 did, dogfood day 0).
		{&model.Model{ID: "claude-fable-5"}, "Fable 5"},
		{&model.Model{ID: "claude-mythos-5"}, "Mythos 5"},
		// But an alpha family with a non-numeric tail is not restyled.
		{&model.Model{ID: "claude-code-guide"}, "claude-code-guide"},
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

// ---------------------------------------------------- v1.1 row grammar

// The chip is a claim that a fan-out is running. Zero is not that claim, and a
// "⑂0" on every Claude row would be noise asserting a fact nobody asked for —
// the same reason an absent gauge draws nothing rather than an empty track.
func TestSubagentChipOnlyAppearsForANonzeroCount(t *testing.T) {
	g := UnicodeGlyphs()
	cases := []struct {
		name string
		s    *model.Session
		want string
	}{
		{"two running", sess(model.VendorClaude, "a", `C:\x\a`, "m", time.Second, withSubagents(2)), "⑂~2"},
		{"measured zero", sess(model.VendorClaude, "b", `C:\x\b`, "m", time.Second, withSubagents(0)), ""},
		{"never counted", sess(model.VendorClaude, "c", `C:\x\c`, "m", time.Second), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := subagentChip(c.s, g); got != c.want {
				t.Errorf("chip = %q, want %q", got, c.want)
			}
		})
	}
}

// The count is exact; "these are running right now" is the inference. ADR-001
// requires the inferred part be visible, so the chip carries the same estimate
// marker the CONTEXT column uses.
func TestSubagentChipCarriesTheEstimateMarker(t *testing.T) {
	g := UnicodeGlyphs()
	derivedRow := sess(model.VendorClaude, "a", `C:\x\a`, "m", time.Second, withSubagents(3))
	if got := subagentChip(derivedRow, g); !strings.Contains(got, "~") {
		t.Errorf("chip = %q, want an estimate marker", got)
	}
	// A hypothetical adapter that could READ the number would not carry one.
	reported := sess(model.VendorClaude, "b", `C:\x\b`, "m", time.Second)
	reported.Subagents = model.Ptr(3)
	if got := subagentChip(reported, g); strings.Contains(got, "~") {
		t.Errorf("a reported count = %q must not be marked an estimate", got)
	}
}

// The chip's width is reserved before the name is truncated. A chip that
// disappeared on a long project name would make the same session look like a
// different kind of session at a different terminal width.
func TestSubagentChipSurvivesLabelTruncation(t *testing.T) {
	g := UnicodeGlyphs()
	long := sess(model.VendorClaude, "id", `C:\src\code\overflow`, "claude-opus-5", time.Second,
		withName("a-really-long-project-name-that-overflows-the-label-column-and-then-some"),
		withSubagents(4))
	for _, w := range []int{12, 20, 34, 59} {
		got := sessionLabel(long, w, g)
		if !strings.Contains(got, "⑂~4") {
			t.Errorf("width %d: chip lost to truncation: %q", w, got)
		}
		if n := lipgloss.Width(got); n > w {
			t.Errorf("width %d: label is %d columns: %q", w, n, got)
		}
	}
}

// The selection is carried by a GLYPH in the leading pad column, not by
// reverse video: §7.1 rule 2 says colour only ever reinforces, and a
// highlight-only cursor vanishes under NO_COLOR.
func TestSelectionIsAGlyphNotAHighlight(t *testing.T) {
	st := v11State(120, 9)
	unselected := Render(st, PlainStyles(), UnicodeGlyphs())
	st.Cursor = 0
	selected := Render(st, PlainStyles(), UnicodeGlyphs())
	if unselected == selected {
		t.Fatal("a selected row renders identically to an unselected one with styles stripped")
	}
	if !strings.Contains(selected, "▸●") {
		t.Errorf("no selection glyph in the frame\n%s", selected)
	}
}

// A monitor must not boot with a row already chosen: that asserts the row
// matters. The mark appears the first time the user asks for it.
func TestNothingIsSelectedUntilAskedFor(t *testing.T) {
	st := v11State(120, 9)
	if st.Cursor != -1 {
		t.Fatalf("Cursor = %d on a fresh state, want -1", st.Cursor)
	}
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); strings.Contains(got, "▸") {
		t.Error("a selection mark rendered before anything was selected")
	}
}

// Every v1.1 glyph has an ASCII form, and none of them collides with a glyph
// that already means something else in that set.
func TestASCIIRowGrammarUsesTheReducedGlyphs(t *testing.T) {
	g := GlyphsFor(true)
	st := v11State(120, 9)
	st.Cursor = 0
	got := Render(st, PlainStyles(), g)
	if !strings.Contains(got, "Y~2") {
		t.Errorf("no ASCII fan-out chip in the frame\n%s", got)
	}
	if !strings.Contains(got, "]*") {
		t.Errorf("no ASCII selection mark in the frame\n%s", got)
	}
	for _, taken := range []string{g.Ellipsis, g.DotLive, g.DotStale} {
		if g.Cursor == taken {
			t.Errorf("the ASCII cursor %q already means something else", g.Cursor)
		}
	}
}

// ------------------------------------------------------------ v1.1 find

func TestFindNarrowsOnNameWorkspaceAndID(t *testing.T) {
	st := healthyState(120, 12)
	cases := []struct {
		query string
		want  int
	}{
		{"", 4},
		{"api", 2},          // acme-api (name), notes-api (workspace basename)
		{"API", 2},          // case-insensitive
		{`C:\src\work`, 1},  // path substring
		{"bbbb", 1},         // the codex session id
		{"nothing here", 0}, // no match hides every row, and says so
	}
	for _, c := range cases {
		st.Query = c.query
		if got := len(visibleSessions(st)); got != c.want {
			t.Errorf("query %q matched %d rows, want %d", c.query, got, c.want)
		}
	}
}

// A monitor that hides rows must say it is hiding them, whether or not the
// mode that applied the filter is still on.
func TestAnAppliedQueryAlwaysAnnouncesItself(t *testing.T) {
	st := healthyState(120, 9)
	st.Query = "api"
	got := Render(st, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(got, `find "api"`) {
		t.Errorf("an applied query is not stated in the footer\n%s", got)
	}
	if !strings.Contains(got, "2 of 4 sessions") {
		t.Errorf("the header count does not reflect the query\n%s", got)
	}
}

// An empty list must name what emptied it. "no active sessions" while a query
// hides four of them is the monitor lying by omission.
func TestAnEmptyResultNamesTheQuery(t *testing.T) {
	st := healthyState(120, 12)
	st.Query = "zzz"
	got := Render(st, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(got, `no sessions matching "zzz"`) {
		t.Errorf("the empty state does not name the query\n%s", got)
	}
}

// The query is text typed by a user and it lands in the footer and the empty
// state. It gets the same treatment as a session name.
func TestFindQueryCannotTearTheFooter(t *testing.T) {
	st := healthyState(120, 12)
	st.Query = "a\u2028b"
	st.Finding = true
	got := Render(st, PlainStyles(), UnicodeGlyphs())
	if strings.ContainsAny(got, "\u2028\u2029") {
		t.Fatal("a separator character reached the footer")
	}
}

// A query longer than the footer must be truncated, never pushed off it:
// joinEnds gives the right slot priority, so an over-long query would vanish
// from the footer while still hiding rows \u2014 the exact silent row-hiding this
// footer exists to prevent.
func TestALongQueryIsTruncatedNotDropped(t *testing.T) {
	for _, w := range []int{60, 72, 80, 120} {
		st := healthyState(w, 12)
		st.Query = strings.Repeat("verylongquery", 12)
		for _, finding := range []bool{true, false} {
			st.Finding = finding
			out := Render(st, PlainStyles(), UnicodeGlyphs())
			if !strings.Contains(out, "verylong") {
				t.Errorf("width %d finding=%v: the query vanished from the footer\n%s", w, finding, out)
			}
			for i, line := range strings.Split(out, "\n") {
				if n := len([]rune(line)); n > w {
					t.Errorf("width %d finding=%v: line %d is %d columns\n%s", w, finding, i, n, line)
				}
			}
		}
	}
}

// What is on screen must be what is being matched. Trimming the displayed
// query would show "acme" while the filter hid every row looking for "acme ".
func TestTheDisplayedQueryIsTheQueryBeingMatched(t *testing.T) {
	st := healthyState(120, 12)
	st.Query = "acme "
	st.Finding = true
	got := Render(st, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(got, "/acme _") {
		t.Errorf("the trailing space the user typed is not on screen\n%s", got)
	}
}

// ------------------------------------------------------------ v1.1 burn

// The forecast renders only where the account quota does — in the header, once
// — and only when it has a basis. The healthy fixture has no samples at all,
// so it must render nothing rather than a placeholder.
func TestNoForecastRendersWithoutABasis(t *testing.T) {
	st := healthyState(120, 9)
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); strings.Contains(got, "basis") {
		t.Errorf("a forecast rendered with no samples behind it\n%s", got)
	}
}

// The projection travels with its scope, always. "~15:41" alone is a
// prediction; "~15:41 · 18m basis" is a measurement with its scope attached.
func TestForecastAlwaysCarriesItsBasis(t *testing.T) {
	f := Forecast{At: pinned.Add(87 * time.Minute), Basis: 18 * time.Minute, Samples: 7}
	got := forecastText(f, pinned, UnicodeGlyphs())
	if got != "~13:27 · 18m basis" {
		t.Errorf("forecastText = %q", got)
	}
	if !strings.HasPrefix(got, "~") {
		t.Error("the forecast is telltale's own computation and must be marked derived")
	}
}

// Render is pure over State, which includes the clock's location: a golden
// must not depend on the CI runner's timezone.
func TestForecastClockUsesTheStateLocation(t *testing.T) {
	f := Forecast{At: pinned.Add(87 * time.Minute), Basis: 18 * time.Minute, Samples: 7}
	east := time.FixedZone("UTC+5", 5*60*60)
	got := forecastText(f, pinned.In(east), UnicodeGlyphs())
	if !strings.HasPrefix(got, "~18:27") {
		t.Errorf("forecastText = %q, want the state's own zone", got)
	}
}

// Attribution survives every width. The full vendor name renders wherever it
// fits (the tighter window join bought it room the first fix's tag spent);
// where it cannot, the two-letter tag carries the same fact — an unlabeled
// `7d 79%` reading as "the fleet" is the defect this pin exists to prevent.
func TestQuotaBlockNamesItsVendor(t *testing.T) {
	st := healthyState(120, 9)
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), st.Width)
	if !strings.Contains(got, "claude ") {
		t.Fatalf("quota block missing vendor label: %q", got)
	}
	if strings.HasPrefix(strings.TrimSpace(got), "5h") {
		t.Fatalf("quota block still unattributed: %q", got)
	}

	// Narrow enough that no full-name level fits: the tag takes over rather
	// than the name vanishing, and the line never overflows the budget.
	for _, w := range []int{55, 50, 40} {
		got = quotaBlock(st, PlainStyles(), UnicodeGlyphs(), w)
		if !strings.HasPrefix(got, "cc ") {
			t.Fatalf("width %d: quota block lost its vendor: %q", w, got)
		}
		if lipgloss.Width(got) > w {
			t.Fatalf("width %d: quota block is %d columns: %q", w, lipgloss.Width(got), got)
		}
	}
}

// One block per vendor with a reading, each labelled — the count of blocks is
// itself a measurement of how many vendors telltale can honestly speak for.
func TestRelayedQuotaRendersEveryVendorItCanSpeakFor(t *testing.T) {
	// At 120 three vendors and four windows fit only at the tag level — every
	// vendor still present, every reading intact.
	st := fleetQuotaState(120, 9)
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), st.Width)
	for _, want := range []string{"ag ", "cc ", "cx ", "gemini-weekly", "79%", "42%", "38%"} {
		if !strings.Contains(got, want) {
			t.Errorf("quota line missing %q: %q", want, got)
		}
	}
	// Wide enough and the full names come back, gauges and all.
	got = quotaBlock(st, PlainStyles(), UnicodeGlyphs(), 160)
	for _, want := range []string{"claude", "codex", "agy"} {
		if !strings.Contains(got, want) {
			t.Errorf("wide quota line missing %q: %q", want, got)
		}
	}
}

// The line never outruns any frame: the dress cascade sheds decoration first,
// and when even the barest level cannot fit, whole trailing blocks go and the
// ellipsis says so (the footer's dropping-is-never-silent rule).
func TestRelayedQuotaNeverOutrunsTheFrame(t *testing.T) {
	st := fleetQuotaState(120, 9)
	for _, w := range []int{MinWidth, 64, 72, 80, 99, 120, 148} {
		got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), w)
		if lipgloss.Width(got) > w {
			t.Errorf("width %d: quota line is %d columns: %q", w, lipgloss.Width(got), got)
		}
	}
	// At the floor the fleet cannot fit whole; the drop must be marked.
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), MinWidth)
	if !strings.Contains(got, "…") {
		t.Errorf("blocks were dropped with no ellipsis saying so: %q", got)
	}
}

// A relayed reading past quotaAgeShown carries its age — the §7.12 basis rule:
// the scope of a claim travels with the number, and "42%" alone would present
// a two-hour-old reading as now. A fresh relay and the scan-fresh transcript
// reading say nothing, because "just now" is noise, not information.
func TestARelayedReadingCarriesItsAge(t *testing.T) {
	st := fleetQuotaState(120, 9)
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), st.Width)
	if !strings.Contains(got, "2h ago") {
		t.Errorf("the stale claude reading hides its age: %q", got)
	}
	if n := strings.Count(got, "ago"); n != 1 {
		t.Errorf("want exactly one age marker (fresh readings say nothing), got %d: %q", n, got)
	}
}

// One vendor, one block: when a vendor's quota is both in a transcript and in
// the relay, the transcript wins — it is re-measured every scan, while the
// relay is as old as the last statusline render.
func TestTheTranscriptReadingOutranksTheRelayForOneVendor(t *testing.T) {
	st := fleetQuotaState(120, 9)
	st.Snap.Account = append(st.Snap.Account, quotacache.Account{
		Vendor:    model.VendorCodex,
		WrittenAt: pinned.Add(-3 * time.Hour),
		Windows:   []model.QuotaWindow{window("seven_day", "7d", 55, 20*time.Hour)},
	})
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), st.Width)
	if strings.Contains(got, "55%") {
		t.Errorf("the stale relayed codex reading rendered over the transcript's: %q", got)
	}
	if n := strings.Count(got, "codex") + strings.Count(got, "cx "); n != 1 {
		t.Errorf("codex appears %d times, want one block: %q", n, got)
	}
}

// Window ids collide across vendors — Claude and Codex both have a
// "seven_day" — so a projection may only ever render inside the block whose
// windows actually fed the sampler. A forecast beside the relayed Claude 7d
// would be Codex's slope pinned to Claude's account.
func TestAForecastNeverCrossesVendors(t *testing.T) {
	st := fleetQuotaState(148, 9)
	resets := pinned.Add(22*time.Hour + 48*time.Minute)
	st.Burn = Burn{
		Source: "codex/0f00dbaa-1234-4a77-9b02-000000000042",
		Series: []BurnSeries{burnSeries("seven_day", 60, 79, 18*time.Minute, 7, &resets)},
	}
	got := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), 200)
	if n := strings.Count(got, "basis"); n != 1 {
		t.Fatalf("want exactly one forecast on the line, got %d: %q", n, got)
	}
	blocks := strings.Split(got, "│")
	for _, b := range blocks {
		if strings.Contains(b, "basis") && !strings.Contains(b, "codex") {
			t.Errorf("a forecast rendered outside the sampled vendor's block: %q", b)
		}
	}
}

// The sampler and the header must read the same windows, or the forecast
// describes a quota that is not on screen.
func TestTheForecastSamplesTheWindowsTheHeaderShows(t *testing.T) {
	st := healthyState(120, 9)
	got := accountQuota(st)
	if len(got) != 2 || got[0].ID != "five_hour" || got[1].ID != "seven_day" {
		t.Fatalf("accountQuota returned %v", got)
	}
	// The most recently active quota-bearing session wins, exactly as the
	// header block picks it.
	st.Snap.Sessions[3].Quota = []model.QuotaWindow{window("weekly", "7d", 5, time.Hour)}
	if got := accountQuota(st); got[0].ID != "five_hour" {
		t.Errorf("accountQuota picked the stale session's windows: %v", got)
	}
}

// ----------------------------------------------------------- shape drift

// driftState is the healthy frame plus one session whose read reported drift,
// with the named vendors rolled up as drifted.
//
// The vendor list is a parameter because the footer notice changes shape with
// how many vendors moved, and the width tests need the worst case. The one
// drifted session is credited to each of them, which no real scan would do —
// this fixture feeds the notice and the width, never the counts, which
// TestTheVendorLineStatesHowMuchOfTheStoreDrifted covers on its own.
func driftState(w, h int, vendors ...model.VendorID) State {
	st := healthyState(w, h)
	drifter := sess(model.VendorCodex, "drifted-row", "", "", time.Minute,
		withDrift("codex-cli 0.146.0", codexCanary))
	st.Snap.Sessions = append(healthy(), drifter)
	st.Snap.Vendors = nil
	for _, v := range vendors {
		st.Snap.Vendors = append(st.Snap.Vendors,
			drifted(v, `%USERPROFILE%\`+string(v), fullCaps, []*model.Session{drifter}))
	}
	return st
}

// THE assertion this change exists for. Drift reached the model in #84 and
// stopped at the detail pane, which means a store that silently stopped
// matching still read as healthy on the screen a person actually looks at.
func TestDriftIsVisibleOnTheGridNotOnlyInTheDetailPane(t *testing.T) {
	clean := Render(healthyState(120, 9), PlainStyles(), UnicodeGlyphs())
	if strings.Contains(clean, "drift") {
		t.Fatalf("a healthy frame mentions drift\n%s", clean)
	}
	got := Render(driftState(120, 9, model.VendorCodex), PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(got, "codex drifted") {
		t.Errorf("a drifted store renders no differently from a healthy one\n%s", got)
	}
}

// The word is carried by the WORD, per §7.1 rule 2: --ascii swaps the notice
// glyph and NO_COLOR drops the hue, and the fact survives both.
func TestTheDriftNoticeSurvivesASCIIAndPlainStyles(t *testing.T) {
	st := driftState(120, 9, model.VendorCodex)
	ascii := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(ascii, "! codex drifted") {
		t.Errorf("the ascii frame does not state the drift\n%s", ascii)
	}
	// Colour is the second signal, so stripping it must not remove the fact —
	// but it must still BE a second signal when it is available.
	coloured := Render(st, NewStyles(true), UnicodeGlyphs())
	if !strings.Contains(coloured, "\x1b[33m") {
		t.Error("the drift notice carries no warning colour")
	}
}

// Naming beats counting until the list stops fitting the footer's share of the
// line. Truncating instead would drop a drifted vendor from the one notice
// whose job is to name them.
func TestTheDriftNoticeNamesVendorsUntilThereAreTooMany(t *testing.T) {
	all := []model.VendorID{
		model.VendorAntigravity, model.VendorClaude, model.VendorCodex,
		model.VendorCursor, model.VendorGemini,
	}
	cases := []struct {
		n    int
		want string
	}{
		{1, "⚠ agy drifted"},
		{2, "⚠ agy, claude drifted"},
		{3, "⚠ 3 vendors drifted"},
		{5, "⚠ 5 vendors drifted"},
	}
	for _, c := range cases {
		got := driftNotice(driftState(120, 9, all[:c.n]...), UnicodeGlyphs())
		if got != c.want {
			t.Errorf("%d drifted vendors: notice = %q, want %q", c.n, got, c.want)
		}
	}
	if got := driftNotice(healthyState(120, 9), UnicodeGlyphs()); got != "" {
		t.Errorf("a healthy snapshot produced a notice: %q", got)
	}
}

// The notice is a fact about the snapshot, not about the visible rows: a filter
// that happens to hide the drifted vendor's sessions does not un-move its store.
func TestTheDriftNoticeSurvivesAFilterThatHidesTheDriftedRows(t *testing.T) {
	st := driftState(120, 9, model.VendorCodex)
	st.Filter = FilterClaude
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); !strings.Contains(got, "codex drifted") {
		t.Errorf("filtering the drifted vendor's rows away silenced the notice\n%s", got)
	}
}

// The notice must render under every body, because the vendor line renders
// under exactly one of them. A warning that comes and goes with whichever pane
// is open is one a reader cannot trust to be there.
func TestTheDriftNoticeRendersUnderEveryBody(t *testing.T) {
	for _, body := range []string{"grid", "help", "detail", "empty"} {
		st := driftState(120, 14, model.VendorCodex)
		switch body {
		case "help":
			st.Help = true
		case "detail":
			st.Detail, st.Cursor = true, 0
		case "empty":
			st.Query = "no-such-session"
		}
		if got := Render(st, PlainStyles(), UnicodeGlyphs()); !strings.Contains(got, "codex drifted") {
			t.Errorf("%s body: the drift notice is missing\n%s", body, got)
		}
	}
}

// noticeLoads are the other things that compete for the footer's notice block.
//
// The first cut of the width test below was shaped to its own fixture: it never
// set Query, Filter, Sort or scan staleness, so the drift notice was always the
// ONLY notice on the line, and the case that actually overflowed — 60 columns
// with a 24-character query and a vendor filter also active — was invisible to
// it. A test that only ever exercises its subject alone cannot see the subject
// pushing something else over the edge.
func noticeLoads() []struct {
	name  string
	apply func(*State)
} {
	stale := func(st *State) {
		st.Snap.At = pinned.Add(-90 * time.Second)
		st.Snap.Err = "Access is denied."
	}
	// Exactly the length the footer truncates a query to, so the notice is at
	// its widest.
	query := func(st *State) { st.Query = "abcdefghijklmnopqrstuvwx" }
	return []struct {
		name  string
		apply func(*State)
	}{
		{"alone", func(*State) {}},
		{"query", query},
		{"filter", func(st *State) { st.Filter = FilterClaude }},
		{"sort", func(st *State) { st.Sort = SortCost }},
		{"stale", stale},
		{"query+filter", func(st *State) { query(st); st.Filter = FilterClaude }},
		{"everything", func(st *State) {
			query(st)
			st.Filter = FilterClaude
			st.Sort = SortCost
			stale(st)
		}},
	}
}

// Every frame must fit the terminal it was asked for, drift notice included.
// The notice block is the side joinEnds KEEPS when the line will not hold both,
// and joinEnds has no truncation path — so a notice that does not fit is a
// notice that runs off the end of the terminal.
func TestADriftedFrameStillFitsEveryTier(t *testing.T) {
	vendorSets := [][]model.VendorID{
		{model.VendorCodex},
		// The widest two-name notice, not the alphabetically first pair:
		// "claude, cursor" is three columns wider than "agy, claude".
		{model.VendorClaude, model.VendorCursor},
		{model.VendorAntigravity, model.VendorClaude, model.VendorCodex,
			model.VendorCursor, model.VendorGemini},
	}
	for _, w := range []int{MinWidth, 61, 72, 80, 99, 100, 120} {
		for _, vs := range vendorSets {
			for _, load := range noticeLoads() {
				for _, ascii := range []bool{false, true} {
					st := driftState(w, 12, vs...)
					load.apply(&st)
					out := Render(st, PlainStyles(), GlyphsFor(ascii))
					for i, line := range strings.Split(out, "\n") {
						if got := len([]rune(line)); got > w {
							t.Errorf("width %d, %d drifted, load %q, ascii=%v: line %d is %d columns\n%s",
								w, len(vs), load.name, ascii, i, got, line)
						}
					}
				}
			}
		}
	}
}

// What the line gives up when it cannot hold everything, stated as a table
// rather than left to whichever notice happened to be appended last.
//
// The rule is joinEnds' own, asked one level down: joinEnds sacrifices the key
// hints because the notices carry what the reader cannot get elsewhere, and the
// notices are ordered among themselves by the same question. Drift is last to
// go because it is the only one on the line that neither clears itself nor can
// be re-read by pressing a key.
func TestTheFooterGivesUpItsCheapestNoticesFirst(t *testing.T) {
	st := driftState(60, 12, model.VendorCodex)
	st.Query = "abcdefghijklmnopqrstuvwx"
	st.Filter = FilterClaude
	st.Sort = SortCost
	st.Snap.At = pinned.Add(-90 * time.Second)

	got := lastLine(Render(st, PlainStyles(), UnicodeGlyphs()))
	// Both warnings survive; sort, the filter and the query do not, and the
	// ellipsis says so.
	for _, want := range []string{"⚠ codex drifted", "⚠ last scan 1m ago", "…"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "sort ") {
		t.Errorf("sort outlived a warning in the footer: %q", got)
	}
	// Dropping is never silent: the ellipsis means here what it means in every
	// other cell of this UI.
	if !strings.HasPrefix(strings.TrimSpace(got), "…") {
		t.Errorf("notices were dropped without the ellipsis saying so: %q", got)
	}
	// Nothing is dropped while it all fits.
	wide := driftState(120, 12, model.VendorCodex)
	wide.Filter = FilterClaude
	wide.Sort = SortCost
	if f := lastLine(Render(wide, PlainStyles(), UnicodeGlyphs())); strings.Contains(f, "…") ||
		!strings.Contains(f, "sort cost") {
		t.Errorf("a footer that fits dropped something anyway: %q", f)
	}
}

// A single notice wider than the whole line is truncated, not dropped: an
// ellipsis on a warning still says a warning is there, and dropping the last
// one would leave a footer quietly claiming nothing is wrong.
func TestTheLastSurvivingNoticeIsTruncatedRatherThanDropped(t *testing.T) {
	st := driftState(60, 12, model.VendorCodex)
	st.Snap.At = pinned.Add(-90 * time.Second)
	st.Snap.Err = strings.Repeat("a very long operating system message. ", 5)

	got := lastLine(Render(st, PlainStyles(), UnicodeGlyphs()))
	if len([]rune(got)) > 60 {
		t.Fatalf("footer is %d columns: %q", len([]rune(got)), got)
	}
	if !strings.Contains(got, "⚠") {
		t.Errorf("the warning was dropped rather than truncated: %q", got)
	}
}

// driftScope renders in the empty state and nowhere else, and neither existing
// width test ever renders that state.
//
// The assertion is DIFFERENTIAL. It was written while the empty state could
// still overflow on its own (centerBlock pads and never truncates, and the
// vendor line carried no budget), so it asserts only that the scope never
// turns a frame that fit into one that does not. The absolute claim — no
// vendor line exceeds the frame at all — is
// TestAVendorErrorIsTruncatedRatherThanTearingTheFrame's.
func TestTheDriftScopeNeverOverflowsAFrameThatFitWithoutIt(t *testing.T) {
	for _, w := range []int{MinWidth, 61, 72, 80, 99, 120} {
		st := driftState(w, 12, model.VendorCodex)
		st.Query = "no-such-session" // empties the grid, so the vendor line renders
		before := widestLine(Render(st, PlainStyles(), UnicodeGlyphs()))

		st.Snap.Vendors[0].Status = StatusWatching
		baseline := widestLine(Render(st, PlainStyles(), UnicodeGlyphs()))

		if baseline <= w && before > w {
			t.Errorf("width %d: the drift scope pushed a fitting frame to %d columns", w, before)
		}
	}
	// And it is really on screen wherever there is room for it.
	st := driftState(120, 12, model.VendorCodex)
	st.Query = "no-such-session"
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); !strings.Contains(got, "1 drifted of 1 read") {
		t.Errorf("the vendor line states no scope at 120 columns\n%s", got)
	}
}

// The empty state's vendor table used to be the one surface with no width
// budget: centerBlock pads and never truncates, so a refused store's OS
// message drew straight past the frame — the empty-unreadable scenario
// rendered 74 columns in a 60-column terminal. The error is the status's
// evidence, so it is truncated rather than dropped (the footer's rule for its
// last surviving notice), and the frame never tears.
func TestAVendorErrorIsTruncatedRatherThanTearingTheFrame(t *testing.T) {
	unreadable := func(w int, err string) State {
		st := NewState()
		st.Now = pinned
		st.Width, st.Height = w, 12
		st.Snap = Snapshot{
			At: pinned,
			Vendors: []VendorView{
				{Vendor: model.VendorClaude, Root: `%USERPROFILE%\.claude\projects`,
					Status: StatusUnreadable, Err: err},
				{Vendor: model.VendorCodex, Root: `%USERPROFILE%\.codex`,
					Status: StatusNotDetected},
			},
		}
		return st
	}

	long := strings.Repeat("The volume for a file has been externally altered. ", 3)
	for _, w := range []int{MinWidth, 61, 72, 80, 99, 120} {
		frame := Render(unreadable(w, long), PlainStyles(), UnicodeGlyphs())
		if widest := widestLine(frame); widest > w {
			t.Errorf("width %d: the empty state draws %d columns", w, widest)
		}
		// Truncated, not dropped: the surviving fragment still ends in the
		// ellipsis that says there is more, on the line that says unreadable.
		for _, l := range strings.Split(frame, "\n") {
			if strings.Contains(l, "unreadable") && !strings.Contains(l, "…") {
				t.Errorf("width %d: the error was dropped rather than truncated: %q", w, l)
			}
		}
	}

	// The short real-world message is untouched wherever it fits: the budget
	// spends nothing on a line that was never going to tear.
	frame := Render(unreadable(120, "Access is denied."), PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(frame, "Access is denied.") {
		t.Errorf("a fitting error was altered:\n%s", frame)
	}
}

// The vendor line must not borrow the header's "n of m sessions" sentence: the
// two land on the same screen with different numerators over different
// populations, and in the borrowed grammar the vendor line reads as a claim
// about how many sessions are SHOWING — which the header directly contradicts.
func TestTheDriftScopeCannotBeReadAsTheHeaderCount(t *testing.T) {
	st := driftState(120, 12, model.VendorCodex)
	st.Query = "no-such-session"
	out := Render(st, PlainStyles(), UnicodeGlyphs())

	header := strings.Split(out, "\n")[0]
	if !strings.Contains(header, "0 of 5 sessions") {
		t.Fatalf("fixture drifted; header = %q", header)
	}
	var vendorLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "drifted") && strings.Contains(l, "%USERPROFILE%") {
			vendorLine = l
		}
	}
	if vendorLine == "" {
		t.Fatal("no vendor line in the empty state")
	}
	if strings.Contains(vendorLine, "sessions") {
		t.Errorf("the vendor line reuses the header's noun: %q", vendorLine)
	}
	if !strings.Contains(vendorLine, "1 drifted of 1 read") {
		t.Errorf("vendor line = %q", vendorLine)
	}
}

func lastLine(frame string) string {
	lines := strings.Split(frame, "\n")
	return lines[len(lines)-1]
}

func widestLine(frame string) int {
	w := 0
	for _, l := range strings.Split(frame, "\n") {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	return w
}

// The vendor line states the SCOPE beside the word. One drifted session out of
// forty is a vendor mid-rollout; forty of forty is a format that moved under
// the whole store, and "drifted" alone cannot tell those apart.
func TestTheVendorLineStatesHowMuchOfTheStoreDrifted(t *testing.T) {
	read := make([]*model.Session, 0, 41)
	for i := 0; i < 40; i++ {
		read = append(read, sess(model.VendorCodex, "healthy-"+strconv.Itoa(i), `C:\x\y`, "m", time.Minute))
	}
	read = append(read, sess(model.VendorCodex, "moved", "", "", time.Minute,
		withDrift("codex-cli 0.146.0", codexCanary)))

	v := drifted(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps, read)
	if v.Status != StatusDrifted {
		t.Fatalf("status = %s, want drifted", v.Status)
	}
	if got := driftScope(v); got != "1 drifted of 41 read" {
		t.Errorf("driftScope = %q", got)
	}
}

// StatusUnreadable is a strictly bigger fact than StatusDrifted — a store we
// could not open tells us nothing about its shape — and it must not be
// downgraded by a stale count left on the view.
func TestUnreadableIsNeverDowngradedToDrifted(t *testing.T) {
	view := VendorView{Vendor: model.VendorCursor, Status: StatusUnreadable, Err: "Access is denied."}
	foldDrift(&view, nil)
	if view.Status != StatusUnreadable {
		t.Errorf("status = %s, want unreadable to survive the roll-up", view.Status)
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
