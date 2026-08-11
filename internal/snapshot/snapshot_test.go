package snapshot

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
)

var update = flag.Bool("update", false, "rewrite the golden documents")

// at is the fixed clock every fixture is built against. Build is pure over its
// arguments, so a constant here is all a golden needs to be stable.
var at = time.Date(2026, 8, 11, 17, 30, 0, 0, time.UTC)

// claudeCaps is a vendor that can source everything this document reports, and
// computes two of them.
var claudeCaps = model.Capabilities{
	Reported: model.NewFieldSet(model.FieldName, model.FieldModel, model.FieldWorkspace,
		model.FieldCost, model.FieldQuota, model.FieldLastActivity),
	Derived: model.NewFieldSet(model.FieldContextPercent, model.FieldSubagents),
}

// codexCaps is a vendor that can source neither cost nor quota nor a context
// percentage. Those three must reach the document as "unsupported", which is a
// different statement from a null this vendor could have filled.
var codexCaps = model.Capabilities{
	Reported: model.NewFieldSet(model.FieldName, model.FieldModel, model.FieldWorkspace,
		model.FieldLastActivity),
}

type sessOpt func(*model.Session)

func withCtx(p float64) sessOpt {
	return func(s *model.Session) {
		s.ContextPercent = model.PercentPtr(p)
		s.Derived = s.Derived.With(model.FieldContextPercent)
	}
}

func withCost(c float64) sessOpt {
	return func(s *model.Session) { s.Cost = model.USDPtr(c) }
}

func withSubagents(n int) sessOpt {
	return func(s *model.Session) {
		s.Subagents = model.Ptr(n)
		s.Derived = s.Derived.With(model.FieldSubagents)
	}
}

func withDrift(s *model.Session) {
	s.Diagnostics = append(s.Diagnostics, "shape drift: two of forty records moved")
}

func sess(v model.VendorID, id string, age time.Duration, opts ...sessOpt) *model.Session {
	s := &model.Session{
		Vendor:       v,
		ID:           id,
		ObservedAt:   at,
		LastActivity: model.TimePtr(at.Add(-age)),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func view(v model.VendorID, st hud.VendorStatus, caps model.Capabilities, sessions, drifted int) hud.VendorView {
	return hud.VendorView{Vendor: v, Status: st, Caps: caps, Sessions: sessions, Drifted: drifted}
}

// withErr attaches the operating system's own message to a vendor line. Only
// StatusUnreadable carries one.
func withErr(v hud.VendorView, msg string) hud.VendorView {
	v.Err = msg
	return v
}

// ------------------------------------------------------------ golden scenarios

func TestGoldenDocuments(t *testing.T) {
	cases := []struct {
		name string
		snap hud.Snapshot
	}{
		// THE load-bearing assertion, in JSON: a session at 0% context and $0
		// cost carries the number 0, and a session whose vendor cannot source
		// either carries null — and says which of the two nulls it is.
		//
		// It is the same property internal/hud pins in its own
		// zero-vs-absent golden, asserted on the surface a program reads. A
		// renderer that collapsed the pair would draw one glyph for both; a
		// serializer that collapses it emits 0 for "we never knew", which is
		// worse, because the reader has no pixels to be suspicious of.
		{name: "zero-vs-absent", snap: hud.Snapshot{
			At: at,
			Vendors: []hud.VendorView{
				view(model.VendorClaude, hud.StatusWatching, claudeCaps, 2, 0),
				view(model.VendorCodex, hud.StatusWatching, codexCaps, 1, 0),
			},
			Sessions: []*model.Session{
				// Measured zero on every axis this vendor can source.
				sess(model.VendorClaude, "aaaa-0001", 5*time.Second,
					withCtx(0), withCost(0), withSubagents(0)),
				// Same vendor, same capabilities, no reading yet: absent NOW.
				sess(model.VendorClaude, "aaaa-0002", 40*time.Minute),
				// Another vendor that can never source them: absent ALWAYS.
				sess(model.VendorCodex, "bbbb-0001", 3*time.Minute),
			},
		}},

		// A fleet with nothing on it. Every list is [] and every measurement is
		// null; no key is missing. A reader must be able to tell an idle fleet
		// from a schema that moved.
		{name: "empty-fleet", snap: hud.Snapshot{
			At: at,
			Vendors: []hud.VendorView{
				view(model.VendorClaude, hud.StatusNotDetected, claudeCaps, 0, 0),
				view(model.VendorCodex, hud.StatusNotDetected, codexCaps, 0, 0),
			},
		}},

		// The ordinary case: live work, relayed account quota, a derived
		// context percentage, and one vendor the operating system refused.
		{name: "watching", snap: hud.Snapshot{
			At: at,
			Vendors: []hud.VendorView{
				view(model.VendorClaude, hud.StatusWatching, claudeCaps, 2, 0),
				withErr(view(model.VendorCodex, hud.StatusUnreadable, codexCaps, 0, 0),
					"open C:\\Users\\x\\.codex\\sessions: Access is denied."),
			},
			Sessions: []*model.Session{
				sess(model.VendorClaude, "aaaa-0001", 10*time.Second,
					withCtx(61.25), withCost(1.5), withSubagents(3)),
				sess(model.VendorClaude, "aaaa-0002", 20*time.Minute,
					withCtx(12.4), withCost(0.125)),
			},
			// The relay speaks for the account even where the ADAPTER has no
			// session-level quota capability. That is why "quota" appears in
			// no capability list — see `reported` in snapshot.go.
			Account: []quotacache.Account{{
				Vendor:    model.VendorClaude,
				WrittenAt: at.Add(-90 * time.Second),
				Windows: []model.QuotaWindow{
					{ID: "five_hour", Label: "5h", UsedPercent: model.PercentPtr(42)},
					// A window the vendor has and has published no figure for.
					// It must stay null; 0% would be a claim of an untouched
					// week that nobody measured.
					{ID: "seven_day", Label: "7d", ResetsAt: model.TimePtr(at.Add(72 * time.Hour))},
				},
			}},
		}},

		// A store that opened, read, and no longer matches the shape the
		// adapter was verified against — plus the scan's own deadline.
		{name: "drifted", snap: hud.Snapshot{
			At:  at,
			Err: "context deadline exceeded",
			Vendors: []hud.VendorView{
				view(model.VendorClaude, hud.StatusDrifted, claudeCaps, 2, 1),
			},
			Sessions: []*model.Session{
				sess(model.VendorClaude, "aaaa-0001", 5*time.Second, withCtx(20)),
				sess(model.VendorClaude, "aaaa-0002", 5*time.Minute, withDrift),
			},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Encode(Build(tc.snap, model.DefaultLivenessThresholds), false)
			if err != nil {
				t.Fatal(err)
			}
			compareGolden(t, tc.name, string(out))
		})
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
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
		t.Fatalf("%v (run: go test ./internal/snapshot -update)", err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got != want {
		t.Errorf("document differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// ------------------------------------------------------------ unit assertions

// decode reads a built document back as generic JSON, which is what a consumer
// actually does. Asserting on the Go struct would pass over a `null` that
// arrived as a missing key.
func decode(t *testing.T, snap hud.Snapshot) map[string]any {
	t.Helper()
	out, err := Encode(Build(snap, model.DefaultLivenessThresholds), false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("the document is not valid JSON: %v", err)
	}
	return doc
}

func vendorBlock(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	for _, raw := range doc["vendors"].([]any) {
		v := raw.(map[string]any)
		if v["vendor"] == id {
			return v
		}
	}
	t.Fatalf("no vendor block for %q", id)
	return nil
}

// TestZeroIsANumberAndAbsentIsNull is the golden test's assertion made
// mechanical: the two states must have different JSON types, not merely
// different bytes.
func TestZeroIsANumberAndAbsentIsNull(t *testing.T) {
	zero := decode(t, hud.Snapshot{
		At:       at,
		Vendors:  []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{sess(model.VendorClaude, "a", time.Second, withCtx(0), withCost(0), withSubagents(0))},
	})
	absent := decode(t, hud.Snapshot{
		At:       at,
		Vendors:  []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{sess(model.VendorClaude, "a", time.Second)},
	})

	for _, field := range []string{"context_pct_max", "cost_usd_total", "subagents_max"} {
		z := vendorBlock(t, zero, "claude")[field]
		a := vendorBlock(t, absent, "claude")[field]
		if n, ok := z.(float64); !ok || n != 0 {
			t.Errorf("%s: a measured zero must serialize as the number 0, got %#v", field, z)
		}
		if a != nil {
			t.Errorf("%s: an absent value must serialize as null, got %#v", field, a)
		}
	}
	if fz, fa := zero["fleet"].(map[string]any), absent["fleet"].(map[string]any); fz["cost_usd_total"] == fa["cost_usd_total"] {
		t.Error("the fleet rollup collapsed zero and absent into the same value")
	}
}

// TestEveryOptionalKeyIsPresent guards the omitempty mistake. A key that
// disappears when its value is absent makes "no reading" indistinguishable
// from "this schema changed", and a consumer cannot tell which it hit.
func TestEveryOptionalKeyIsPresent(t *testing.T) {
	doc := decode(t, hud.Snapshot{
		At:      at,
		Vendors: []hud.VendorView{view(model.VendorCodex, hud.StatusNotDetected, codexCaps, 0, 0)},
	})
	for _, k := range []string{"schema_version", "generated_at", "scan_error", "fleet", "vendors"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("the document dropped the key %q", k)
		}
	}
	v := vendorBlock(t, doc, "codex")
	for _, k := range []string{"vendor", "status", "error", "sessions", "live", "drifted",
		"context_pct_max", "cost_usd_total", "subagents_max", "last_activity",
		"quota", "quota_read_at", "estimated", "unsupported"} {
		if _, ok := v[k]; !ok {
			t.Errorf("the vendor block dropped the key %q", k)
		}
	}
	fleet := doc["fleet"].(map[string]any)
	for _, k := range []string{"sessions", "live", "idle", "stale", "unknown",
		"vendors_watching", "vendors_not_detected", "vendors_unreadable", "vendors_drifted",
		"context_pct_max", "cost_usd_total", "last_activity"} {
		if _, ok := fleet[k]; !ok {
			t.Errorf("the fleet rollup dropped the key %q", k)
		}
	}
}

// TestEmptyStatesAreDefinitive: an empty list is [], never null. A reader must
// never have to handle two spellings of "nothing".
func TestEmptyStatesAreDefinitive(t *testing.T) {
	doc := decode(t, hud.Snapshot{At: at})
	if doc["vendors"] == nil {
		t.Fatal("vendors is null on an empty fleet; it must be []")
	}
	if n := len(doc["vendors"].([]any)); n != 0 {
		t.Fatalf("vendors has %d entries on an empty fleet", n)
	}
	doc = decode(t, hud.Snapshot{
		At:      at,
		Vendors: []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 0, 0)},
	})
	v := vendorBlock(t, doc, "claude")
	for _, k := range []string{"quota", "estimated", "unsupported"} {
		if v[k] == nil {
			t.Errorf("%s is null with nothing to report; it must be []", k)
		}
	}
}

// TestUnsupportedNamesExactlyTheFieldsTheVendorCannotSource keeps the two
// kinds of absence apart. "can't know" is a capability statement and belongs
// in this list; "absent now" is this reading and must not.
func TestUnsupportedNamesExactlyTheFieldsTheVendorCannotSource(t *testing.T) {
	doc := decode(t, hud.Snapshot{
		At: at,
		Vendors: []hud.VendorView{
			view(model.VendorClaude, hud.StatusWatching, claudeCaps, 0, 0),
			view(model.VendorCodex, hud.StatusWatching, codexCaps, 0, 0),
		},
	})
	if got := strs(vendorBlock(t, doc, "claude")["unsupported"]); len(got) != 0 {
		t.Errorf("claude sources every reported field; unsupported = %v", got)
	}
	// "quota" is absent from this list on purpose: the quota array is relayed
	// from the account, not sourced from a session, so a session-level
	// capability cannot speak for it (see `reported`).
	want := []string{"context_pct", "cost", "subagents"}
	got := strs(vendorBlock(t, doc, "codex")["unsupported"])
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("codex unsupported = %v, want %v", got, want)
	}
}

// TestDerivedValuesCarryTheEstimateMarker is the honest-gauge rule on this
// surface: a computed value may travel, and it may not travel as if the vendor
// had reported it.
func TestDerivedValuesCarryTheEstimateMarker(t *testing.T) {
	doc := decode(t, hud.Snapshot{
		At:      at,
		Vendors: []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{
			sess(model.VendorClaude, "a", time.Second, withCtx(50), withCost(2), withSubagents(1)),
		},
	})
	v := vendorBlock(t, doc, "claude")
	got := strs(v["estimated"])
	want := []string{"context_pct", "subagents"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("estimated = %v, want %v — cost is reported and must not be marked", got, want)
	}
}

// TestNoSessionContentReachesTheDocument is the read/write boundary asserted
// the way internal/cursorhook asserts its own: plant markers in the fields
// that carry content and require that none of them survives serialization.
func TestNoSessionContentReachesTheDocument(t *testing.T) {
	s := sess(model.VendorClaude, "aaaa-0001", time.Second, withCtx(10))
	s.Name = model.Ptr("MARKER-SESSION-NAME")
	s.WorkspaceDir = model.Ptr(`C:\src\MARKER-WORKSPACE`)
	s.Model = &model.Model{ID: "MARKER-MODEL"}
	s.Diagnostics = append(s.Diagnostics, "MARKER-DIAGNOSTIC")
	s.Extras = append(s.Extras, model.Extra{Label: "MARKER-LABEL", Value: "MARKER-VALUE"})

	out, err := Encode(Build(hud.Snapshot{
		At:       at,
		Vendors:  []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{s},
	}, model.DefaultLivenessThresholds), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "MARKER") {
		t.Errorf("session content reached the document:\n%s", out)
	}
	// The session id is content too: it names a conversation, and the counts
	// are what this surface is for.
	if strings.Contains(string(out), "aaaa-0001") {
		t.Errorf("a session id reached the document:\n%s", out)
	}
}

// TestSpendIsNotRendered pins the held display (§7.16's amendment). The relay
// stays wired and the HUD keeps reading it; this document must not become the
// place that display quietly returns.
func TestSpendIsNotRendered(t *testing.T) {
	s := sess(model.VendorClaude, "a", time.Second)
	s.Tokens = &model.TokenCounts{Input: 123456, Output: 654321}
	out, err := Encode(Build(hud.Snapshot{
		At:       at,
		Vendors:  []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{s},
	}, model.DefaultLivenessThresholds), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"123456", "654321", "token"} {
		if strings.Contains(string(out), marker) {
			t.Errorf("a token count reached the document (%q):\n%s", marker, out)
		}
	}
}

// TestLivenessCountsSumToTheSessionCount: the rollup is what a reader trusts
// instead of doing arithmetic, so it has to add up.
func TestLivenessCountsSumToTheSessionCount(t *testing.T) {
	noActivity := sess(model.VendorClaude, "d", 0)
	noActivity.LastActivity = nil

	doc := decode(t, hud.Snapshot{
		At:      at,
		Vendors: []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 4, 0)},
		Sessions: []*model.Session{
			sess(model.VendorClaude, "a", 5*time.Second),
			sess(model.VendorClaude, "b", 5*time.Minute),
			sess(model.VendorClaude, "c", 5*time.Hour),
			noActivity,
		},
	})
	f := doc["fleet"].(map[string]any)
	sum := f["live"].(float64) + f["idle"].(float64) + f["stale"].(float64) + f["unknown"].(float64)
	if sum != f["sessions"].(float64) {
		t.Errorf("liveness counts sum to %v, sessions = %v", sum, f["sessions"])
	}
	if f["unknown"].(float64) != 1 {
		t.Errorf("a session with no activity and no hint must count as unknown, not stale: %#v", f)
	}
}

// TestBuildIsPureOverItsArguments: two builds of the same scan produce the
// same bytes. Reaching for time.Now() inside Build would make every golden
// flaky in a way that only shows in CI.
func TestBuildIsPureOverItsArguments(t *testing.T) {
	snap := hud.Snapshot{
		At:       at,
		Vendors:  []hud.VendorView{view(model.VendorClaude, hud.StatusWatching, claudeCaps, 1, 0)},
		Sessions: []*model.Session{sess(model.VendorClaude, "a", time.Second, withCtx(3))},
	}
	first, err := Encode(Build(snap, model.DefaultLivenessThresholds), true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := Encode(Build(snap, model.DefaultLivenessThresholds), true)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("Build is not pure:\n%s\n%s", first, second)
	}
}

// TestCompactIsOneLine: both forms end with a newline, and the compact form
// has exactly one — a document that is a line in a pipe.
func TestCompactIsOneLine(t *testing.T) {
	out, err := Encode(Build(hud.Snapshot{At: at}, model.DefaultLivenessThresholds), true)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), "\n"); n != 1 {
		t.Errorf("the compact document has %d newlines, want 1", n)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Error("the compact document does not end with a newline")
	}
}

func strs(v any) []string {
	var out []string
	for _, raw := range v.([]any) {
		out = append(out, raw.(string))
	}
	return out
}
