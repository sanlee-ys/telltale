package hud

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	grokadapter "github.com/sanlee-ys/telltale/internal/adapter/grok"
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

// withTokens sets the measured per-session token counts the fleet usage view
// sums (§7.17). It is deliberately separate from withExtras even though the
// real adapter sets both from the same two variables: a fixture that could only
// set the display strings could not exercise the sum at all, and one that could
// only set the integers would stop pinning what the detail pane shows.
func withTokens(in, out int64) sessionOpt {
	return func(s *model.Session) { s.Tokens = &model.TokenCounts{Input: in, Output: out} }
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

// agySession is an Antigravity CLI row as the real adapter produces one: NAME
// is absent, because the vendor writes no human title anywhere a public repo
// may read — the only free text on its disk is prompt content — so the row
// falls back to the workspace basename (§9.11-style HUD fallback, ruled
// 2026-08-12; the adapter declares FieldName CapNone rather than fill it from
// the conversation id). The model display string is the vendor's own, long
// enough that the 13-column MODEL cell truncates it, which is what the HUD
// really shows and therefore what the golden must pin.
//
// The token counts are set BOTH ways, the way the adapter sets them: the extras
// carry the display strings the detail pane shows, and Tokens carries the same
// two measurements as integers for the fleet sum. The values agree — 40,512
// formats as "40k" through the adapter's own rounding — because a fixture whose
// two halves disagreed would let a bug that dropped one of them keep passing.
func agySession(age time.Duration) *model.Session {
	return sess(model.VendorAntigravity, "4c8b21a7-0e35-4a12-9f6b-000000000001",
		`C:\src\code\example-app`, "", age,
		withModel(&model.Model{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash (High)"}),
		withExtras("uncached in", "40k", "output", "380", "generations", "2"),
		withTokens(40512, 380))
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

// spendState is the frame §7.16's amendment exists for: the relay is running,
// its file holds a real accumulated total, and NOTHING on screen shows it.
//
// The fixture keeps the Snapshot.Spend entry on purpose rather than deleting
// it. "The display was retired" and "the machinery was ripped out" are two
// different changes, and only one of them was ruled; a fixture with no total in
// it could not tell them apart, and every assertion about the retirement would
// pass on a build that had lost the relay entirely.
//
// What the frame must still show is Codex's quota on the quota line and
// Cursor's quota nowhere — Cursor has no account quota and saying so by absence
// is the honest answer, which is the half of TestSpendIsNeverRenderedAsQuota
// that outlived the line it was written for.
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
				CacheWriteTokens: model.Ptr(int64(62004)),
			},
		}},
	}
	return st
}

// grokState is a grok-only frame: two sessions from the same store, one titled
// and one from a headless run that the vendor never titled, and one with no
// signals.json yet so its context is absent rather than zero.
func grokState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorGrok, "00000000-1111-7222-8333-000000000001",
				`C:\src\code\telltale`, "grok-4.5", 20*time.Second,
				withName("Adapter Field Map Review"), withCtx(7),
				withExtras("ctx tokens", "39k", "ctx window", "500k",
					"turn cost", "$0.0747", "turn tokens", "143k")),
			// A `--single` run: session_summary is "", there is no
			// generated_title key at all, and no turn boundary has written
			// signals.json. Both absences are the vendor having nothing to say.
			sess(model.VendorGrok, "00000000-1111-7222-8333-000000000002",
				`C:\src\code\example-app`, "grok-4.5", 6*time.Minute,
				withExtras("turn cost", "$0.0306", "turn tokens", "19k")),
		},
		Vendors: []VendorView{
			watching(model.VendorGrok, `%USERPROFILE%\.grok\sessions`,
				grokadapter.New().Capabilities()),
		},
	}
	return st
}

// usageFleetState is the frame §7.17 exists for: every source state telltale
// can be in, at once, so the view has to keep them apart in one screen.
//
//   - CODEX — quota from its own store, re-measured this scan.
//   - CLAUDE — quota relayed by the statusline two hours ago, and saying so.
//     No sessions: a vendor can be on this surface for its reading alone.
//   - AGY — quota relayed a minute ago, fresh enough to say nothing about age,
//     AND a spend line, summed over its two conversations' measured token
//     counts. The only vendor carrying both kinds of claim, which makes its
//     block the one where blurring them would show first.
//   - CURSOR — no quota anywhere and never will be without a network call, and
//     since 2026-08-09 no spend line either. Its relay is still accumulating
//     into Snapshot.Spend, which this fixture carries and this view ignores.
//   - GEMINI — sessions on this machine and nothing to say about either: the
//     absence line, with its reason.
//   - GROK — the fleet's sixth vendor (#183), here from the day it arrived. It
//     took the FALLBACK absence sentence until 2026-08-09 and now has one of
//     its own, measured (§3.9a): the same shape as gemini's block and a
//     different verdict behind it. It is also the vendor whose store holds real
//     money and still gets no spend line, for the reason §7.17 Declined states.
//
// A vendor with no sessions, no quota and no total is deliberately NOT here: it
// does not appear at all, which is what TestUsageOmitsAVendorItHasNothingToSayAbout
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
			agySession(2 * time.Minute),
			// A second agy conversation, so the spend line is visibly a SUM
			// over more than one and its count is not decoration.
			sess(model.VendorAntigravity, "9b1e07f4-2a66-4c30-8d51-000000000002",
				`C:\src\code\telltale`, "", 8*time.Minute,
				withModel(&model.Model{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash (High)"}),
				withExtras("uncached in", "1M", "output", "12k", "generations", "9"),
				withTokens(1_204_880, 12_744)),
			// A third that has not called a model yet: no Tokens at all. It is
			// in the vendor's session count and NOT in the spend line's, which
			// is the difference between a window that names what it summed and
			// one that quietly folds an absence in as a zero.
			sess(model.VendorAntigravity, "0d5a3c62-77bb-4e19-91af-000000000003",
				`C:\src\code\agent-ops`, "", 40*time.Minute),
			sess(model.VendorGrok, "00000000-1111-7222-8333-000000000001",
				`C:\src\code\telltale`, "grok-4.5", 20*time.Second,
				withName("Adapter Field Map Review"), withCtx(7)),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
				(&claudecode.Adapter{}).Capabilities()),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
			watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`,
				(&gemini.Adapter{}).Capabilities()),
			watching(model.VendorAntigravity, `%USERPROFILE%\.gemini\antigravity-cli`,
				agyadapter.New().Capabilities()),
			watching(model.VendorCursor, `%APPDATA%\Cursor\User`,
				(&cursoradapter.Adapter{}).Capabilities()),
			watching(model.VendorGrok, `%USERPROFILE%\.grok\sessions`,
				grokadapter.New().Capabilities()),
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
		// The retired display's data, still arriving. Kept in the fixture so the
		// goldens pin "read and not rendered" rather than "not read" — the two
		// look identical on screen and are entirely different changes.
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
				CacheWriteTokens: model.Ptr(int64(62004)),
			},
		}},
	}
	st.Usage = true
	return st
}

// weekState is the fleet fixture through the §7.19 lens, with agy's account
// entry widened to all four observed buckets. The widening is the point of the
// fixture: the render then exercises every leg of weeklyWindows at once — the
// "-weekly" suffix selecting two windows under one vendor (and the second on a
// continuation row), the last-window leg on Claude and Codex, and the dedupe
// where agy's last window is already in the suffix set. The five-hour buckets
// are in the fixture precisely so the golden proves they do NOT render.
func weekState(w, h int) State {
	st := usageFleetState(w, h)
	st.Usage = false
	st.Week = true
	st.Snap.Account[1].Windows = []model.QuotaWindow{
		window("3p-5h", "3p-5h", 0, 4*time.Hour+57*time.Minute),
		window("3p-weekly", "3p-weekly", 0, 6*24*time.Hour+23*time.Hour),
		window("gemini-5h", "gemini-5h", 12, 4*time.Hour+54*time.Minute),
		window("gemini-weekly", "gemini-weekly", 38, 6*24*time.Hour+23*time.Hour),
	}
	return st
}

// weekStaleState is §7.17's nineteen-hour field report seen through the week
// page: the escalation has to survive the lens, because this page has no
// block heading to carry it — the suffix on the row is all there is (§7.15).
func weekStaleState(w, h int) State {
	st := usageStaleRelayState(w, h)
	st.Usage = false
	st.Week = true
	return st
}

// usageStaleRelayState is the field report of 2026-08-09, reconstructed.
//
// A Claude relay entry written nineteen hours ago, reporting 15% of the seven-
// day window. Every part of that is a state the product genuinely reaches: the
// five-hour window is gone because quotacache drops a window whose reset has
// passed (§7.15's self-expiry), the entry itself survives because it is inside
// the 24h ceiling, and the reset it reports is still four days out so nothing
// upstream has any reason to touch it. It rendered at full confidence and it
// was read as current — the account was at 44%.
//
// agy's minute-old reading sits in the same frame deliberately: the escalation
// has to be visible as a DIFFERENCE between two relayed blocks, not as a new
// coat of paint on the concept "relayed".
func usageStaleRelayState(w, h int) State {
	st := usageFleetState(w, h)
	st.Snap.Account[0] = quotacache.Account{
		Vendor:    model.VendorClaude,
		WrittenAt: pinned.Add(-19 * time.Hour),
		Windows: []model.QuotaWindow{
			window("seven_day", "7d", 15, 4*24*time.Hour+7*time.Hour),
		},
	}
	return st
}

// usageTallState is the void, reconstructed: the fleet fixture in a terminal
// tall enough that the page used to stop around 40% and fade into nothing.
//
// It pins BOTH halves of the §7.17 fix and it has to, because either alone
// still reads as unfinished — the gaps between vendor blocks widen to two rows
// (usageAir), and the closing rule is drawn where the content stops rather than
// at the frame's bottom edge, so the leftover rows are visibly outside the page.
func usageTallState(w, h int) State { return usageFleetState(w, h) }

// usageModelsState is the models census's own scenario, and it holds every case
// the row has to get right in one frame:
//
//   - CLAUDE — four sessions, three distinct models, one of them run twice. The
//     row says three names, not four, and it says them in the grid's own
//     normalized spelling rather than as raw ids.
//   - CODEX — one session, one model, and a quota window under it, so the
//     census and a reading sit in the same column without being confusable.
//   - GEMINI — a session whose adapter sourced no model at all. The vendor still
//     has a block, because it is on this machine; it has no models row, because
//     absent renders absent and an em dash would claim telltale looked at a
//     model and found it nameless.
func usageModelsState(w, h int) State {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = w, h
	st.Snap = Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001",
				`C:\src\code\telltale`, "claude-opus-5", 12*time.Second, withName("telltale")),
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000002",
				`C:\src\code\agent-ops`, "claude-sonnet-4-5", 3*time.Minute, withName("agent-ops")),
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000003",
				`C:\src\code\desk`, "claude-opus-5", 6*time.Minute, withName("desk")),
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000004",
				`C:\src\code\learning-notes`, "claude-haiku-4-5", 20*time.Minute, withName("learning-notes")),
			sess(model.VendorCodex, "0f00dbaa-1234-4a77-9b02-000000000042",
				`C:\src\code\notes-api`, "gpt-5.1-codex", 4*time.Minute, withName("notes-api"),
				withQuota(window("seven_day", "7d", 79, 22*time.Hour+48*time.Minute))),
			sess(model.VendorGemini, "session-2026-08-02T09-58-0a1b2c3d",
				`c:\src\code\portfolio`, "", 9*time.Minute, withName("portfolio")),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`,
				(&claudecode.Adapter{}).Capabilities()),
			watching(model.VendorCodex, `%USERPROFILE%\.codex`, fullCaps),
			watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`,
				(&gemini.Adapter{}).Capabilities()),
		},
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
			// 18 rows: 17 fitted the overlay exactly until the week page's
			// `w` row joined the key list, and the height moves with the list
			// so the frame keeps showing its own exit.
			st := healthyState(120, 18)
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
		// sources model/workspace/last_activity only — no on-disk title, so its
		// row falls back to the workspace basename the same way Gemini's does
		// when a session has none of its own (ruled 2026-08-12); and Cursor
		// sources a context percentage the vendor itself wrote down, which is
		// why its CONTEXT cell carries a bar and no estimate marker beside the
		// Codex row's computed one.
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
		// Public HUD hero. Eight sessions across every adapter the grid has —
		// including Grok, whose row carries a vendor-reported (unmarked)
		// context percentage and no COST cell, because the store only writes
		// per-turn dollars and never a session total (§3.9a). Height is 14 so
		// the eighth row still fits at 120 columns without shedding the footer.
		{name: "readme", state: func() State {
			st := NewState()
			st.Now = pinned
			st.Width, st.Height = 120, 14
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
					// Grok reports its own context percentage (CapReported) and
					// writes no session cost — the 7% is unmarked on purpose,
					// next to Codex's `~69.8%` estimate marker. Empty cost is
					// CapNone, not zero.
					sess(model.VendorGrok, "00000000-1111-7222-8333-000000000001",
						`C:\src\code\telltale`, "grok-4.5", 45*time.Second,
						withName("Adapter Field Map Review"), withCtx(7),
						withExtras("ctx tokens", "39k", "ctx window", "500k",
							"turn cost", "$0.0747", "turn tokens", "143k")),
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
					watching(model.VendorGrok, `%USERPROFILE%\.grok\sessions`,
						grokadapter.New().Capabilities()),
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

		// The token relay after its display was retired (§7.16's amendment).
		// The SAME fixture that produced the "spend-cursor" render until
		// 2026-08-09, with its relayed total still in the snapshot — and the
		// header is two lines instead of three, with the total nowhere on
		// screen. Codex's quota is still on the quota line and Cursor still has
		// none there, which is the half of the old claim that outlived the line.
		{name: "cursor-without-spend", state: func() State {
			return spendState(120, 10)
		}},

		// The grok seam (§3.9a), and it is here for one claim: this is the
		// vendor whose store holds a real dollar figure, and the COST column is
		// still dropped. The money is a per-TURN reading with no session total
		// anywhere on disk, so it lives in the detail pane's extras where its
		// label says which turn it belongs to, and the grid asserts nothing.
		// The context bar beside it is UNMARKED — grok reports the percentage
		// rather than deriving it, the second vendor after Cursor to do so.
		{name: "grok-row", state: func() State {
			return grokState(120, 9)
		}},

		// ------------------------------------------------- the usage view

		// Every source state at once (§7.17): scan-fresh quota, relayed quota
		// carrying its age, relayed quota fresh enough not to, a relayed
		// reading and a scan-derived token total under one vendor, and three
		// vendors with nothing saying which kind of nothing each one is.
		{name: "usage-fleet", state: func() State { return usageFleetState(120, 28) }},

		// The same view at the 60-column floor. The gauges are gone — the grid's
		// own shed order, because the bar re-states a number that is still on
		// screen — and every fact survives, including the relayed reading's age
		// and the spend total's window.
		{name: "usage-floor", state: func() State { return usageFleetState(60, 28) }},

		// ASCII. The distinction between a reading against a limit and a count
		// with no limit is carried by words and by which vocabulary each line
		// uses, so nothing about it depends on the Unicode set.
		{name: "usage-ascii", ascii: true, state: func() State { return usageFleetState(120, 28) }},

		// Nothing measured anywhere: a sentence, not a table of dashes.
		{name: "usage-empty", state: func() State { return usageEmptyState(120, 10) }},

		// The models census: dedupe across four Claude sessions, a vendor whose
		// sessions carry no model at all (absent renders absent, and absent here
		// is no row rather than an em dash), and the grid's own normalization —
		// `claude-opus-5` reads `Opus 5` on both surfaces.
		{name: "usage-models", state: func() State { return usageModelsState(120, 14) }},

		// The nineteen-hour reading that started this (§7.17). Same fixture as
		// usage-fleet with Claude's relay aged past the fleet's shortest quota
		// window: the age gains the warning glyph and the sentence says why,
		// while agy's minute-old reading beside it is untouched.
		{name: "usage-stale-relay", state: func() State { return usageStaleRelayState(120, 27) }},

		// The same escalation in the reduced set. The claim is carried by the
		// glyph and the sentence, so `!` replaces `⚠` and nothing else moves.
		{name: "usage-stale-ascii", ascii: true, state: func() State { return usageStaleRelayState(120, 27) }},

		// The void (§7.17, amended 2026-08-09). The same fleet fixture in a
		// terminal with rows to spare: the blocks breathe into two-row gaps
		// instead of crowding the top, and the closing rule sits under the last
		// block rather than at the frame's bottom, so what is left over reads as
		// unused terminal instead of an unfinished page.
		{name: "usage-tall", state: func() State { return usageTallState(120, 52) }},

		// ------------------------------------------------- the week page

		// The scoping glance (§7.19): every vendor on one screen, slow windows
		// only. Claude and Codex each show their last (longest) window; agy
		// shows both vendor-named weekly pools, the second on a continuation
		// row; the four five-hour buckets in the fixture render nowhere; the
		// three quota-less vendors keep §7.17's absence sentences; spend is
		// absent by design.
		{name: "week", state: func() State { return weekState(120, 18) }},

		// The nineteen-hour reading through the lens: the row's suffix carries
		// the word, the glyph and the age, while agy's minute-old rows beside
		// it carry nothing (§7.15's age rule with no heading to ride on).
		{name: "week-stale", state: func() State { return weekStaleState(120, 18) }},

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

// ------------------------------------ the token relay, retired (§7.16)

// spendLineOf returns a line carrying a spend claim, or "" — found by the verb,
// which is the thing §7.16 says must always be on such a line and therefore the
// thing that cannot be missing from one that exists.
func spendLineOf(render string) string {
	for _, line := range strings.Split(render, "\n") {
		if strings.Contains(line, "spent") {
			return line
		}
	}
	return ""
}

// The ruling, pinned. Cursor's token total is still relayed, still cached and
// still read by every scan — and no surface renders it, at any width, in either
// glyph set, with the usage view open or closed.
//
// This is asserted over the WHOLE frame rather than over the header alone,
// because "retired" here means retired everywhere the §7.16 total used to
// reach: the header had one line and the usage view had another, and a change
// that removed only one of them would leave the number on screen while every
// header assertion passed.
func TestTheCursorSpendDisplayIsRetiredEverywhere(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		for _, w := range []int{200, 120, 100, 80, 60} {
			for _, usage := range []bool{false, true} {
				st := spendState(w, 14)
				st.Usage = usage
				got := Render(st, PlainStyles(), g)
				if line := spendLineOf(got); line != "" {
					t.Errorf("ascii=%v width=%d usage=%v: cursor's retired total rendered:\n%s",
						ascii, w, usage, line)
				}
				// The counts themselves, in case a future line loses the verb
				// before it loses the number. 48.0k is the fixture's input
				// total and 1.9M its cache-read total.
				for _, n := range []string{"48.0k", "1.9M"} {
					if strings.Contains(got, n) {
						t.Errorf("ascii=%v width=%d usage=%v: a relayed token count survived (%s):\n%s",
							ascii, w, usage, n, got)
					}
				}
			}
		}
	}
}

// The other half of the ruling, and the half a "delete the feature" change
// would silently break: the RELAY is not retired. The scan still reads the
// cache into the snapshot, so reinstating the display is a call site rather
// than a re-plumb.
//
// Asserted on the snapshot rather than on a file, because what was ruled on is
// the render: the cache's own reader has its own tests in internal/usagecache
// and they were not touched.
func TestTheRetiredDisplayStillHasItsRelayUnderneath(t *testing.T) {
	st := spendState(120, 12)
	if len(st.Snap.Spend) != 1 || st.Snap.Spend[0].Vendor != model.VendorCursor {
		t.Fatalf("the fixture lost the relayed total the retirement is defined against: %+v", st.Snap.Spend)
	}
	if st.Snap.Spend[0].InputTokens == 0 || st.Snap.Spend[0].Turns == 0 {
		t.Errorf("the relayed entry carries no measurement to have been retired: %+v", st.Snap.Spend[0])
	}
}

// grok's collector (§7.16a) fills the same snapshot field the cursor relay
// does, and its display is HELD under the same ruling that retired cursor's —
// wired write-to-read, rendered nowhere. Pinned separately from the cursor
// test because the entry's SHAPE differs (requests for turns, reasoning for
// cache-write) and a renderer reinstated for one shape could miss the other.
func TestTheGrokSpendRelayRendersNowhere(t *testing.T) {
	withGrokSpend := func(w, h int) State {
		st := grokState(w, h)
		st.Snap.Spend = []usagecache.Total{{
			Vendor: model.VendorGrok,
			Entry: usagecache.Entry{
				Vendor:          string(model.VendorGrok),
				Since:           pinned.Add(-9 * time.Minute),
				WrittenAt:       pinned.Add(-40 * time.Second),
				Requests:        6,
				InputTokens:     81292,
				OutputTokens:    224,
				CacheReadTokens: 10240,
				ReasoningTokens: model.Ptr(int64(168)),
			},
		}}
		return st
	}
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		for _, w := range []int{200, 120, 80, 60} {
			for _, usage := range []bool{false, true} {
				st := withGrokSpend(w, 14)
				st.Usage = usage
				got := Render(st, PlainStyles(), g)
				if line := spendLineOf(got); line != "" {
					t.Errorf("ascii=%v width=%d usage=%v: grok's held total rendered:\n%s",
						ascii, w, usage, line)
				}
				// The counts themselves, in case a future line loses the verb
				// before it loses the number. 81.2k is the fixture's input
				// total, 10.2k its cache-read total.
				for _, n := range []string{"81.2k", "10.2k", "reasoning"} {
					if strings.Contains(got, n) {
						t.Errorf("ascii=%v width=%d usage=%v: a relayed figure survived (%s):\n%s",
							ascii, w, usage, n, got)
					}
				}
			}
		}
	}
	// And the relay half: the entry is in the snapshot, carrying a real
	// measurement, so reinstating a display stays a call site.
	st := withGrokSpend(120, 12)
	if len(st.Snap.Spend) != 1 || st.Snap.Spend[0].Requests == 0 || st.Snap.Spend[0].InputTokens == 0 {
		t.Fatalf("the fixture lost the relayed total the hold is defined against: %+v", st.Snap.Spend)
	}
}

// The half of TestSpendIsNeverRenderedAsQuota that outlived the line it was
// written for. Cursor's quota is ABSENT and must never be faked into the quota
// block — retiring the spend display changes nothing about that, and the
// vendor's disappearance from one line is exactly the moment a renderer would
// be tempted to give it a home on the other.
func TestCursorIsStillGivenNoQuotaBlock(t *testing.T) {
	g := UnicodeGlyphs()
	st := spendState(120, 10)
	quota := quotaBlock(st, PlainStyles(), g, st.Width)
	if strings.Contains(strings.ToLower(quota), "cursor") || strings.Contains(quota, "cu ") {
		t.Errorf("cursor was given a quota block it has no source for:\n%s", quota)
	}
	if !strings.Contains(quota, "codex") {
		t.Errorf("the vendor that DOES have quota lost its block:\n%s", quota)
	}
}

// The header is back to at most two lines, which is the whole of what the
// ruling bought. A third line that reappears for any reason is a regression
// against the reason the display was retired rather than merely moved.
func TestTheHeaderIsAtMostTwoLines(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 60} {
		st := spendState(w, 14)
		lay := resolveLayout(st.Width, true, true)
		quota := quotaBlock(st, PlainStyles(), UnicodeGlyphs(), st.Width)
		lines := headerLines(st, lay, quota, PlainStyles(), UnicodeGlyphs())
		if len(lines) > 2 {
			t.Errorf("width %d: the header grew back to %d lines:\n%s",
				w, len(lines), strings.Join(lines, "\n"))
		}
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

// usageSpendClaimOf returns the whole of a vendor's spend claim: the row with
// the verb on it, plus the continuation row the window wraps to at narrow
// widths.
//
// The two rows are one claim and every assertion below is about the claim, not
// about a line — a test that only looked at the verb's row would report the
// window "shed" at exactly the widths where it did not, and would go on passing
// if the continuation were dropped for real. The spend line is the LAST row of
// its vendor block, so a non-empty row immediately after it is its
// continuation and can be nothing else.
func usageSpendClaimOf(body []string) string {
	for i, line := range body {
		if !strings.Contains(line, "spent") {
			continue
		}
		if i+1 < len(body) && strings.TrimSpace(body[i+1]) != "" {
			return line + "\n" + body[i+1]
		}
		return line
	}
	return ""
}

// The load-bearing assertion of this surface, and §7.1 principle 1 applied to
// it: a vendor with no quota reading never renders a number. "0%" would say the
// account has used none of its allowance, which is a measurement telltale did
// not make and, for Gemini, Cursor and grok, one that does not exist to be
// made.
func TestUsageNeverRendersAQuotaLessVendorAsZero(t *testing.T) {
	g := UnicodeGlyphs()
	for _, line := range usageBodyOf(t, usageFleetState(120, 28), g) {
		for _, v := range []string{" gemini", " cursor", " grok"} {
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

// §7.16's vocabulary rule on the one surface that still renders a spend line —
// and now the ONLY surface, which makes this test the whole of that guarantee.
// agy's block puts a relayed percentage and a token count four lines apart
// under one vendor name, and proximity is exactly what makes this the riskier
// render the header's ever was.
func TestUsageSpendBorrowsNoneOfQuotasVocabulary(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		var spent string
		for _, line := range usageBodyOf(t, usageFleetState(120, 28), g) {
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
//
// For a scan-derived sum the window is TWO claims and both have to survive: how
// many sessions were added up, and that they are the ones on disk right now.
// Shedding the second would leave a number a reader will reasonably assume is
// monotonic — and this one goes down when a conversation is deleted.
func TestUsageTotalNeverPrintsWithoutItsWindow(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 72, 60} {
		spent := usageSpendClaimOf(usageBodyOf(t, usageFleetState(w, 28), UnicodeGlyphs()))
		if spent == "" {
			t.Errorf("width %d: the spend line vanished entirely", w)
			continue
		}
		if !strings.Contains(spent, "2 sessions") {
			t.Errorf("width %d: the count the sum was taken over shed:\n%s", w, spent)
		}
		if !strings.Contains(spent, "on disk") {
			t.Errorf("width %d: the scan-not-a-meter qualifier shed:\n%s", w, spent)
		}
		if !strings.Contains(spent, "uncached in 1.2M") || !strings.Contains(spent, "out 13.1k") {
			t.Errorf("width %d: a token count shed or was mislabelled:\n%s", w, spent)
		}
	}
}

// The spend claim is facts with no decoration left to spend, so where it will
// not fit on one row it takes two rather than dropping half of itself — and
// neither row may run past the frame, which is the bug that made the wrap
// necessary in the first place.
func TestUsageSpendWrapsRatherThanOverflowing(t *testing.T) {
	for _, w := range []int{200, 120, 100, 90, 80, 72, 65, 60} {
		body := usageBodyOf(t, usageFleetState(w, 28), UnicodeGlyphs())
		claim := usageSpendClaimOf(body)
		if claim == "" {
			t.Errorf("width %d: no spend claim at all", w)
			continue
		}
		for _, line := range strings.Split(claim, "\n") {
			if got := lipgloss.Width(line); got > w-1 {
				t.Errorf("width %d: a spend row is %d columns wide:\n%s", w, got, line)
			}
		}
		// Wrapped or not, the claim is whole.
		for _, want := range []string{"uncached in", "out", "2 sessions", "on disk"} {
			if !strings.Contains(claim, want) {
				t.Errorf("width %d: the claim lost %q:\n%s", w, want, claim)
			}
		}
	}

	// And the wrap buys dress back rather than merely surviving: a row to
	// itself fits a window level the shared row could not.
	narrow := usageSpendClaimOf(usageBodyOf(t, usageFleetState(60, 28), UnicodeGlyphs()))
	if !strings.Contains(narrow, "\n") {
		t.Fatalf("the 60-column render did not wrap, so this test pins nothing:\n%s", narrow)
	}
	if !strings.Contains(narrow, "summed across 2 sessions on disk") {
		t.Errorf("the continuation row shed dress it had room for:\n%s", narrow)
	}
}

// The sum's arithmetic, asserted on the numbers rather than on the frame: two
// of agy's three conversations carry measured counts and the third has never
// called a model. It contributes to neither the sum nor the count — folding it
// in as a zero would put a session in the window that contributed nothing to
// the total, which is the "zero is not absent" rule (§4a.1) applied to a
// denominator instead of to a cell.
func TestUsageSumsOnlyTheSessionsThatMeasuredSomething(t *testing.T) {
	st := usageFleetState(120, 28)

	agy := 0
	for _, s := range st.Snap.Sessions {
		if s.Vendor == model.VendorAntigravity {
			agy++
		}
	}
	if agy != 3 {
		t.Fatalf("the fixture no longer has the un-measured conversation this pins: %d agy sessions", agy)
	}

	got, ok := scanSpendByVendor(st)[model.VendorAntigravity]
	if !ok {
		t.Fatal("agy produced no scan-derived total at all")
	}
	if got.sessions != 2 {
		t.Errorf("the window counted %d sessions, want the 2 that measured something", got.sessions)
	}
	if want := int64(40512 + 1_204_880); got.in != want {
		t.Errorf("input sum is %d, want %d", got.in, want)
	}
	if want := int64(380 + 12_744); got.out != want {
		t.Errorf("output sum is %d, want %d", got.out, want)
	}

	// And the other direction: a vendor whose sessions measured nothing has no
	// entry rather than a zeroed one, so no zero can reach the screen with a
	// window attached to make it look counted.
	for _, v := range []model.VendorID{model.VendorGrok, model.VendorCursor, model.VendorGemini} {
		if _, ok := scanSpendByVendor(st)[v]; ok {
			t.Errorf("%s produced a total from sessions that carry no token counts", v)
		}
	}
}

// The count agrees with itself. "2 sessions" is a plural because there are two;
// one would have to read "1 session", because a view whose entire argument is
// that it is careful cannot render "1 sessions".
func TestUsageSpendWindowAgreesWithItsCount(t *testing.T) {
	st := usageFleetState(120, 28)
	var kept []*model.Session
	dropped := false
	for _, s := range st.Snap.Sessions {
		if s.Vendor == model.VendorAntigravity && s.Tokens != nil && !dropped {
			dropped = true
			continue
		}
		kept = append(kept, s)
	}
	st.Snap.Sessions = kept

	spent := usageSpendClaimOf(usageBodyOf(t, st, UnicodeGlyphs()))
	if spent == "" {
		t.Fatal("one measured conversation should still produce a spend line")
	}
	if !strings.Contains(spent, "1 session on disk") || strings.Contains(spent, "1 sessions") {
		t.Errorf("a one-session window did not agree with its count:\n%s", spent)
	}
}

// §4a.1 on this surface: the three ways a vendor can have no quota are three
// different facts, and a reader deciding whether to go and DO something needs
// them apart. "The statusline writes it" is an action; "its store holds
// experiment values" is a closed door.
func TestUsageKeepsTheKindsOfAbsenceApart(t *testing.T) {
	body := strings.Join(usageBodyOf(t, usageFleetState(120, 28), UnicodeGlyphs()), "\n")
	for _, want := range []string{
		// structurally absent, with the measurement behind the verdict.
		"its store holds experiment values, not usage",
		// structurally absent, the other vendor, in its own words.
		"no quota reaches disk anywhere telltale can read",
		// structurally absent, and the newest of the three (§3.9a, measured
		// 2026-08-09). It has its own sentence rather than borrowing Cursor's:
		// the two verdicts are the same shape and different measurements, and a
		// shared sentence would claim grok's store holds Statsig experiment
		// values, which is a thing nobody looked for there.
		"no window, no ordinal, no reset time on its disk",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the usage body never says %q:\n%s", want, body)
		}
	}
	// And the generic fallback is not what grok got. A vendor whose seam has
	// been surveyed must not render the sentence reserved for one nobody has
	// looked at (§7.17's known limitation).
	if strings.Contains(body, "no quota telltale can read") {
		t.Errorf("a surveyed vendor rendered the un-surveyed fallback wording:\n%s", body)
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
	seam := usageFleetState(120, 28)
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
	st := usageFleetState(120, 28)
	body := strings.Join(usageBodyOf(t, st, UnicodeGlyphs()), "\n")
	// claude has a relayed reading and no sessions: it appears.
	if !strings.Contains(body, " claude") {
		t.Errorf("a vendor with a relayed reading and no sessions was dropped:\n%s", body)
	}
	// Now take the readings away, and agy's sessions with them — agy is the one
	// vendor here holding BOTH kinds of evidence, so it takes both removals to
	// leave it with nothing. Neither vendor then has anything telltale measured,
	// and an absence line is for a vendor that is HERE and silent, not for one
	// that is not here at all. Both must vanish outright.
	st.Snap.Account = nil
	var kept []*model.Session
	for _, s := range st.Snap.Sessions {
		if s.Vendor != model.VendorAntigravity {
			kept = append(kept, s)
		}
	}
	st.Snap.Sessions = kept
	body = strings.Join(usageBodyOf(t, st, UnicodeGlyphs()), "\n")
	for _, gone := range []string{" agy", " claude"} {
		if strings.Contains(body, gone) {
			t.Errorf("a vendor with no sessions and no reading still got a block (%s):\n%s", gone, body)
		}
	}
	// The vendors that ARE here are untouched by that.
	for _, present := range []string{" codex", " gemini", " cursor", " grok"} {
		if !strings.Contains(body, present) {
			t.Errorf("a vendor with sessions lost its block (%s):\n%s", present, body)
		}
	}
}

// A vendor with sessions and a scan-derived total but NO quota reading is still
// on the surface — the spend line is evidence of its own. This is the case the
// retirement made newly reachable: agy alone can hold a total with no reading
// beside it now, and it must not fall out of the view with the reading.
func TestUsageKeepsAVendorThatOnlyHasATotal(t *testing.T) {
	st := usageFleetState(120, 28)
	st.Snap.Account = nil
	var agy string
	for _, line := range usageBodyOf(t, st, UnicodeGlyphs()) {
		if strings.HasPrefix(line, " agy") {
			agy = line
		}
	}
	if agy == "" {
		t.Fatalf("a vendor with sessions and a token total lost its block:\n%s",
			strings.Join(usageBodyOf(t, st, UnicodeGlyphs()), "\n"))
	}
	// And its heading still speaks about QUOTA, which is now absent — the
	// asymmetry that keeps a spend-bearing block legible.
	if !strings.Contains(agy, "no quota relayed yet") {
		t.Errorf("a vendor left with only a total did not say why its quota is missing:\n%s", agy)
	}
}

// Fixed fleet order, never sorted by usage. Position is the navigation, so a
// vendor moving must mean a vendor was added or removed — not that another
// vendor's percentage crossed it.
func TestUsageOrderIsTheFleetOrderNotTheReadings(t *testing.T) {
	st := usageFleetState(120, 28)
	want := []string{" claude", " codex", " gemini", " agy", " cursor", " grok"}

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
	narrow := strings.Join(usageBodyOf(t, usageFleetState(60, 28), g), "\n")
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

// -------------------------------------- the §7.17 reading pass (2026-08-09)

// The second rule weight is a CHARACTER before it is a style, so it has to
// survive the reduced set — and it can only do that on a mark nothing else has
// claimed. This enumerates the whole claimed set, council's own test one
// package over, so the next glyph cannot be added without meeting it.
func TestTheHeavyRuleHasAnUnclaimedASCIIPartner(t *testing.T) {
	a := asciiGlyphs()
	claimed := []struct{ what, glyph string }{
		{"the live dot", a.DotLive}, {"the idle dot", a.DotIdle}, {"the stale dot", a.DotStale},
		{"the gauge fill", a.Fill}, {"the light rule and gauge track", a.Track},
		{"the zone separator", a.Sep}, {"the absent marker", a.Absent},
		{"the ellipsis", a.Ellipsis}, {"the reset prefix", a.Reset},
		{"the warning prefix", a.Warn}, {"the selection cursor", a.Cursor},
		{"the fan-out mark", a.Fork}, {"the fact separator", a.Mid},
		{"the find caret", a.Caret},
	}
	for _, c := range claimed {
		if a.RuleHeavy == c.glyph {
			t.Errorf("the heavy rule is %q, which is already %s — a rule weight that also "+
				"means something else is not a weight", a.RuleHeavy, c.what)
		}
	}
	for i, f := range a.Spinner {
		if a.RuleHeavy == f {
			t.Errorf("the heavy rule is %q, which is spinner frame %d", a.RuleHeavy, i)
		}
	}
	// And it has to read as a DOUBLED light rule rather than as a different
	// symbol, in both sets. That is the one property this glyph needs and the
	// reason `=` was chosen over the other unclaimed marks.
	if a.RuleHeavy != "=" || UnicodeGlyphs().RuleHeavy != "━" {
		t.Errorf("the heavy rule pair moved off council's (%q/%q) — §7.1 principle 5 says "+
			"these are one product, and a second heavy-rule character is a second alphabet",
			UnicodeGlyphs().RuleHeavy, a.RuleHeavy)
	}
}

// §9.26's scarcity argument, asserted as a COUNT on the rendered frame rather
// than as a property of the one call site. A second weight is worth exactly
// what it is rare, and a rule on every vendor block would spend it fifteen
// times a screen to restate an indent.
func TestOnlyTheUsageTitleDrawsTheHeavyRule(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		count := func(st State) (lines, runs int) {
			for _, l := range strings.Split(Render(st, PlainStyles(), g), "\n") {
				if n := strings.Count(l, g.RuleHeavy); n > 0 {
					lines++
					runs += strings.Count(l, " "+g.RuleHeavy) // one leading boundary per run
				}
			}
			return
		}

		usage := usageFleetState(120, 28)
		if lines, runs := count(usage); lines != 1 || runs != 1 {
			t.Errorf("ascii=%v: the usage view draws the heavy rule on %d lines in %d runs, want 1 and 1",
				ascii, lines, runs)
		}
		// Every other body: none at all. The frame's own rules stay the interior
		// weight, so a heading inside the outline can never be mistaken for it.
		others := map[string]State{
			"grid":   healthyState(120, 12),
			"help":   func() State { st := healthyState(120, 20); st.Help = true; return st }(),
			"detail": func() State { st := healthyState(120, 20); st.Detail = true; return st }(),
			"empty":  func() State { st := usageEmptyState(120, 12); st.Usage = false; return st }(),
		}
		for name, st := range others {
			if lines, _ := count(st); lines != 0 {
				t.Errorf("ascii=%v: the %s body drew the heavy rule on %d lines", ascii, name, lines)
			}
		}
	}
}

// One grid, not five per-vendor layouts. Claude's `5h` gauge starts where
// codex's `7d` gauge starts and where agy's `gemini-weekly` gauge starts, and
// the percentages and countdowns line up under each other across blocks —
// which is the whole difference between a page a reader scans down one column
// of and five little tables they have to re-find their place in.
func TestUsageFactsShareOneColumnGridAcrossVendors(t *testing.T) {
	g := UnicodeGlyphs()
	for _, w := range []int{120, 99, 80, 60} {
		labelAt := map[int][]string{}
		contAt := map[int][]string{}
		pctAt := map[int][]string{}
		resetAt := map[int][]string{}
		gaugeAt := map[int][]string{}
		value := usageIndent + usageLabel + usageGap

		for _, line := range usageBodyOf(t, usageFleetState(w, 28), g) {
			if strings.TrimSpace(line) == "" || !strings.HasPrefix(line, strings.Repeat(" ", usageIndent)) {
				continue
			}
			r := []rune(line)
			// A CONTINUATION row — the spend line's window where it did not fit
			// beside the counts (§7.17's amendment) — has no label, so it starts
			// in the value column rather than in the label one. That is the grid
			// being obeyed, not broken: an empty label cell is still a label
			// cell. It is bucketed separately and held to the value column
			// exactly, because the failure this test exists to catch would look
			// identical — a row that landed there by drifting rather than by
			// hanging.
			if at := indexOfFirstNonSpace(r, 0); at == value {
				contAt[at] = append(contAt[at], line)
				continue
			}
			labelAt[indexOfFirstNonSpace(r, 0)] = append(labelAt[indexOfFirstNonSpace(r, 0)], line)
			if i := runeIndex(r, '%'); i >= 0 {
				pctAt[i] = append(pctAt[i], line)
			}
			if i := runeIndex(r, []rune(g.Reset)[0]); i >= 0 {
				resetAt[i] = append(resetAt[i], line)
			}
			if i := indexOfAny(r, append([]string{g.Fill, g.Track}, g.Eighths...)); i >= 0 {
				gaugeAt[i] = append(gaugeAt[i], line)
			}
			// The gap between the label cell and the value is structural, not
			// incidental: a label one cell too long anywhere would shear every
			// column to its right on that row alone.
			for c := usageIndent + usageLabel; c < value && c < len(r); c++ {
				if r[c] != ' ' {
					t.Errorf("width %d: a label ran into the value column:\n%s", w, line)
					break
				}
			}
		}

		// Right-aligned cells (the percentage) and left-aligned ones (the gauge,
		// the models census, the spend facts) are checked by the column their
		// own content lands in, because a right-aligned number's first glyph
		// legitimately moves with the number's width.
		for what, cols := range map[string]map[int][]string{
			"the label column":    labelAt,
			"the percentage":      pctAt,
			"the reset countdown": resetAt,
			"the gauge":           gaugeAt,
		} {
			if len(cols) > 1 {
				t.Errorf("width %d: %s lands in %d different columns across vendors:\n%s",
					w, what, len(cols), formatColumns(cols))
			}
		}
		for col := range labelAt {
			if col != usageIndent {
				t.Errorf("width %d: labels start at column %d, not usageIndent (%d)", w, col, usageIndent)
			}
		}
		for col := range contAt {
			if col != value {
				t.Errorf("width %d: a continuation row hangs at column %d, not the shared value column (%d)",
					w, col, value)
			}
		}
		for col := range gaugeAt {
			if col != value {
				t.Errorf("width %d: the gauge starts at column %d, not the shared value column (%d)",
					w, col, value)
			}
		}
	}
}

func indexOfFirstNonSpace(r []rune, from int) int {
	for i := from; i < len(r); i++ {
		if r[i] != ' ' {
			return i
		}
	}
	return -1
}

func indexOfAny(r []rune, glyphs []string) int {
	for i, c := range r {
		for _, gl := range glyphs {
			if gl != "" && c == []rune(gl)[0] {
				return i
			}
		}
	}
	return -1
}

func runeIndex(r []rune, want rune) int {
	for i, c := range r {
		if c == want {
			return i
		}
	}
	return -1
}

func formatColumns(cols map[int][]string) string {
	var keys []int
	for k := range cols {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  col %d: %q\n", k, cols[k][0])
	}
	return b.String()
}

// The half of San's original ask that §7.17 shipped without: which models
// actually did the work under each vendor. Every rule on that row is the
// honest-gauge rule in different clothes, so they are pinned together.
func TestUsageNamesTheModelsItSaw(t *testing.T) {
	st := usageModelsState(120, 14)
	body := usageBodyOf(t, st, UnicodeGlyphs())

	var claude, codex string
	inGemini := false
	geminiRows := 0
	for _, line := range body {
		switch {
		case strings.HasPrefix(line, " claude"), strings.HasPrefix(line, " codex"):
			inGemini = false
		case strings.HasPrefix(line, " gemini"):
			inGemini = true
			continue
		}
		if inGemini && strings.TrimSpace(line) != "" {
			geminiRows++
		}
		if !strings.Contains(line, "models ") {
			continue
		}
		if claude == "" {
			claude = line
		} else if codex == "" {
			codex = line
		}
	}

	// Deduped, sorted, and in the grid's own normalized spelling. Four sessions,
	// three names, and never "claude-opus-5" beside "Opus 5".
	want := "Haiku 4.5, Opus 5, Sonnet 4.5"
	if !strings.Contains(claude, want) {
		t.Errorf("the models row is %q, want the deduped sorted list %q", strings.TrimSpace(claude), want)
	}
	if strings.Contains(claude, "claude-opus-5") {
		t.Errorf("a raw model id reached the models row; the grid normalizes it:\n%s", claude)
	}
	if n := strings.Count(claude, "Opus 5"); n != 1 {
		t.Errorf("two sessions on one model listed it %d times:\n%s", n, claude)
	}
	if !strings.Contains(codex, "gpt-5.1-codex") {
		t.Errorf("codex lost its model: %q", strings.TrimSpace(codex))
	}

	// A vendor whose sessions carry no model gets no row — not an em dash.
	// An em dash claims telltale looked at a model and could not name it.
	if geminiRows != 0 {
		t.Errorf("gemini has sessions with no model and still drew %d fact rows", geminiRows)
	}
	joined := strings.Join(body, "\n")
	if strings.Contains(joined, "models         "+UnicodeGlyphs().Absent) {
		t.Errorf("an absent model census rendered as an absent VALUE:\n%s", joined)
	}

	// Only what is in this snapshot. Take the sessions away and the census goes
	// with them, even though the vendor still has a quota reading to show.
	st2 := usageFleetState(120, 28)
	st2.Snap.Sessions = nil
	for _, line := range usageBodyOf(t, st2, UnicodeGlyphs()) {
		if strings.Contains(line, "models") {
			t.Errorf("a models row survived a snapshot with no sessions — the census is "+
				"remembering:\n%s", line)
		}
	}
}

// Overflow is announced, never clipped. `+3 more` is a count telltale measured;
// a list cut with an ellipsis leaves the reader unable to tell whether one name
// went missing or nine.
func TestUsageModelOverflowSaysHowManyItDropped(t *testing.T) {
	g := UnicodeGlyphs()
	names := []string{"Haiku 4.5", "Opus 5", "Opus 5[1m]", "Sonnet 4.5", "Sonnet 5"}

	// Wide: everything fits and nothing is announced.
	full := usageModelsRow(names, 120, PlainStyles(), g)
	for _, n := range names {
		if !strings.Contains(full, n) {
			t.Errorf("width 120: %q dropped from a row with room for it:\n%s", n, full)
		}
	}
	if strings.Contains(full, "more") {
		t.Errorf("width 120: a complete list still claimed an overflow:\n%s", full)
	}

	// At the floor the row has 36 cells. It keeps whole names and says how many
	// it could not take.
	floor := usageModelsRow(names, 60, PlainStyles(), g)
	if lipgloss.Width(floor) > 59 {
		t.Errorf("the models row overran the 60-column floor (%d):\n%s", lipgloss.Width(floor), floor)
	}
	if !strings.Contains(floor, "+3 more") {
		t.Errorf("the floor row does not say how many models it dropped:\n%s", floor)
	}
	if !strings.Contains(floor, "Haiku 4.5, Opus 5") {
		t.Errorf("the floor row cut a name short instead of dropping it whole:\n%s", floor)
	}
	if strings.Contains(floor, g.Ellipsis) {
		t.Errorf("a name was clipped where a count would have been honest:\n%s", floor)
	}
	// And the marker's own width is reserved rather than hoped for: the count
	// must be right, not merely present.
	if strings.Contains(floor, "Sonnet") {
		t.Errorf("the row kept a name it then claimed to have dropped:\n%s", floor)
	}
}

// The field report of 2026-08-09, as an assertion. A nineteen-hour-old relayed
// reading rendered at the same weight as a fresh one and was acted on; the age
// was present and it was not loud.
//
// The escalation is carried by the WORD and the GLYPH first (§7.1 rule 2), so
// this checks the plain and ascii renders for the fact and the coloured render
// only for the second signal.
func TestARelayedReadingGetsLouderAsItAges(t *testing.T) {
	aged := func(d time.Duration) State {
		st := usageStaleRelayState(120, 27)
		st.Snap.Account[0].WrittenAt = pinned.Add(-d)
		return st
	}

	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		body := strings.Join(usageBodyOf(t, aged(19*time.Hour), g), "\n")
		for _, want := range []string{
			g.Warn + " 19h ago",                            // the glyph, beside the fact
			"older than the fleet's shortest quota window", // the reason, in words
		} {
			if !strings.Contains(body, want) {
				t.Errorf("ascii=%v: the over-age reading never says %q:\n%s", ascii, want, body)
			}
		}
		// The escalation is per reading, not per relay: agy's minute-old block is
		// in the same frame and must be untouched.
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, " agy") && strings.Contains(line, g.Warn) {
				t.Errorf("ascii=%v: a fresh relayed reading was escalated too:\n%s", ascii, line)
			}
		}
	}

	// The threshold is quotaAgeWarn exactly, and it is the fleet's shortest
	// quota window rather than a tuned number.
	if quotaAgeWarn != 5*time.Hour {
		t.Errorf("quotaAgeWarn is %v; §7.17 argues it from Claude's five_hour window", quotaAgeWarn)
	}
	g := UnicodeGlyphs()
	below := strings.Join(usageBodyOf(t, aged(quotaAgeWarn-time.Minute), g), "\n")
	if strings.Contains(below, g.Warn) {
		t.Errorf("a reading inside the shortest window was escalated:\n%s", below)
	}
	if !strings.Contains(below, "· 4h ago") {
		t.Errorf("a reading below the threshold lost its plain age:\n%s", below)
	}
	at := strings.Join(usageBodyOf(t, aged(quotaAgeWarn), g), "\n")
	if !strings.Contains(at, g.Warn+" 5h ago") {
		t.Errorf("a reading exactly at the threshold did not escalate:\n%s", at)
	}

	// Colour is the second signal, and it is the footer's own warning token.
	coloured := Render(aged(19*time.Hour), NewStyles(true), g)
	if !strings.Contains(coloured, "\x1b[33m") {
		t.Error("the over-age reading carries no warning colour")
	}
	// It never escalates past warn. quotacache's 24h drop is a disappearance,
	// not a louder warning, and there is no honest boundary between the two.
	if strings.Contains(coloured, "\x1b[31m"+"quota relayed") {
		t.Error("the relayed heading reached the critical band; §7.17 gives it one step")
	}
	// And the reading itself is untouched: escalating the AGE must not restyle
	// the percentage, whose severity is a statement about the account.
	plain := strings.Join(usageBodyOf(t, aged(19*time.Hour), g), "\n")
	if !strings.Contains(plain, "15%") {
		t.Errorf("the over-age block stopped rendering its reading:\n%s", plain)
	}
}

// The same field report, on the surface it was actually read from (§7.17
// amended). The `u` view escalating while the glance line stayed muted left the
// product loud where a reader looks on purpose and quiet where they merely
// glance, which is backwards.
func TestTheHeaderEscalatesAnOverAgeReadingToo(t *testing.T) {
	aged := func(d time.Duration) State {
		st := usageStaleRelayState(120, 27)
		st.Snap.Account[0].WrittenAt = pinned.Add(-d)
		return st
	}

	// The word and the glyph carry it in both glyph sets — the reduced one is
	// the harder case, since `!` is a weaker mark than `⚠`.
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		got := quotaBlock(aged(19*time.Hour), PlainStyles(), g, 120)
		if !strings.Contains(got, g.Warn+" "+quotaAgeWord+" 19h ago") {
			t.Errorf("ascii=%v: the header renders an over-age reading quietly: %q", ascii, got)
		}
		// Per reading, not per relay: agy's minute-old block shares the line.
		if n := strings.Count(got, g.Warn); n != 1 {
			t.Errorf("ascii=%v: want one escalated block, got %d: %q", ascii, n, got)
		}
	}

	// The threshold is §7.17's, shared rather than restated — a second copy is
	// how the header and the view would disagree about one reading.
	g := UnicodeGlyphs()
	below := quotaBlock(aged(quotaAgeWarn-time.Minute), PlainStyles(), g, 120)
	if strings.Contains(below, g.Warn) {
		t.Errorf("a reading inside the fleet's shortest window was escalated: %q", below)
	}
	if !strings.Contains(below, "· 4h ago") {
		t.Errorf("a reading below the threshold lost its plain age: %q", below)
	}
	if !strings.Contains(quotaBlock(aged(quotaAgeWarn), PlainStyles(), g, 120),
		g.Warn+" "+quotaAgeWord+" 5h ago") {
		t.Error("a reading exactly at the threshold did not escalate")
	}

	// The word and the age survive every level down to the barest; only the
	// reason sheds. The alarm is the fact, the argument is the decoration.
	for _, w := range []int{MinWidth, 64, 72, 80, 99, 120, 148, 200} {
		got := quotaBlock(aged(19*time.Hour), PlainStyles(), g, w)
		if lipgloss.Width(got) > w {
			t.Errorf("width %d: escalated quota line is %d columns: %q", w, lipgloss.Width(got), got)
		}
		// MinWidth drops whole trailing blocks; claude may be one of them.
		if w > MinWidth && !strings.Contains(got, g.Warn+" "+quotaAgeWord+" 19h ago") {
			t.Errorf("width %d: the escalation shed something it may not: %q", w, got)
		}
	}
	// The reason rides the most dressed level, which a single-vendor line
	// reaches. If nothing can reach it, it is a clause that never renders.
	solo := aged(19 * time.Hour)
	solo.Snap.Account = solo.Snap.Account[:1]
	solo.Snap.Sessions = nil
	if got := quotaBlock(solo, PlainStyles(), g, 120); !strings.Contains(got, usageAgeReason) {
		t.Errorf("the reason never renders on any header line: %q", got)
	}

	// Colour is the second signal, and the reading keeps its own severity: the
	// percentage speaks for the account, the suffix for the measurement.
	coloured := quotaBlock(aged(19*time.Hour), NewStyles(true), g, 120)
	if !strings.Contains(coloured, "\x1b[33m") {
		t.Error("the over-age header reading carries no warning colour")
	}
	if !strings.Contains(coloured, "15%") {
		t.Errorf("the over-age header block stopped rendering its reading: %q", coloured)
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
		out := Render(usageFleetState(w, 28), PlainStyles(), UnicodeGlyphs())
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
