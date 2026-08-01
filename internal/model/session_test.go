package model

import (
	"strings"
	"testing"
	"time"
)

// All fixture data in this file is synthesized: fake session ids, fake names,
// fake paths. Real transcript content never enters this repo (public).

// newFixture returns a healthy Claude-shaped session and the capabilities its
// adapter would declare. Tests mutate the copy they get.
func newFixture(now time.Time) (*Session, Capabilities) {
	caps := Capabilities{
		Reported: NewFieldSet(FieldName, FieldModel, FieldWorkspace, FieldCost, FieldQuota, FieldLastActivity),
		Derived:  NewFieldSet(FieldContextPercent),
	}
	s := &Session{
		Vendor:         VendorClaude,
		ID:             "sess-0000",
		ObservedAt:     now,
		Name:           Ptr("wire up the gauge"),
		Model:          &Model{ID: "model-a", DisplayName: "Model A"},
		WorkspaceDir:   Ptr(`C:\Users\dev\code\telltale\`),
		ContextPercent: PercentPtr(41.5),
		Cost:           USDPtr(1.25),
		Quota: []QuotaWindow{
			{ID: "five_hour", Label: "5h", UsedPercent: PercentPtr(23), ResetsAt: UnixTimePtr(1785000000)},
			// Present-but-unreported window: renders "—", never 0%.
			{ID: "seven_day", Label: "7d"},
		},
		LastActivity: TimePtr(now.Add(-30 * time.Second)),
		Derived:      NewFieldSet(FieldContextPercent),
	}
	return s, caps
}

func TestValidateAcceptsHealthySession(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	s, caps := newFixture(now)
	if err := caps.Validate(); err != nil {
		t.Fatalf("capabilities rejected: %v", err)
	}
	if err := s.Validate(caps); err != nil {
		t.Fatalf("healthy session rejected: %v", err)
	}
}

// Validate is the machine-checkable form of the honest-gauge rule. Each case
// here is a way an adapter could quietly claim more than its data supports.
func TestValidateRejectsDishonestSessions(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		want string
		mut  func(*Session)
	}{
		{
			name: "value for a field the adapter declared unsupported",
			want: "declared unsupported",
			mut:  func(s *Session) { s.LivenessHint = LivenessPtr(LivenessLive) },
		},
		{
			name: "derived marking the adapter never declared",
			want: "not declared derived",
			mut:  func(s *Session) { s.Derived = s.Derived.With(FieldCost) },
		},
		{
			name: "field marked derived but carrying no value",
			want: "carrying no value",
			mut:  func(s *Session) { s.ContextPercent = nil },
		},
		{
			name: "field both present and degraded",
			want: "present and degraded",
			mut:  func(s *Session) { s.Degraded = NewFieldSet(FieldCost) },
		},
		{
			name: "percentage out of range instead of dropped",
			want: "out of range",
			mut:  func(s *Session) { s.ContextPercent = PercentPtr(101) },
		},
		{
			name: "absence encoded as the zero time",
			want: "zero time",
			mut:  func(s *Session) { s.LastActivity = TimePtr(time.Time{}) },
		},
		{
			name: "snapshot with no observation time",
			want: "ObservedAt",
			mut:  func(s *Session) { s.ObservedAt = time.Time{} },
		},
		{
			name: "duplicate quota window id",
			want: "duplicate quota window",
			mut: func(s *Session) {
				s.Quota = append(s.Quota, QuotaWindow{ID: "five_hour", Label: "5h"})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, caps := newFixture(now)
			tc.mut(s)
			err := s.Validate(caps)
			if err == nil {
				t.Fatalf("Validate accepted a session that %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// Liveness boundaries are the HUD's, applied identically to every vendor, and
// an unknowable liveness stays unknown rather than defaulting to a claim.
func TestLivenessBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	th := DefaultLivenessThresholds

	for _, tc := range []struct {
		age  time.Duration
		want Liveness
	}{
		{0, LivenessLive},
		{th.Live, LivenessLive}, // boundary is inclusive
		{th.Live + time.Second, LivenessIdle},
		{th.Idle, LivenessIdle},
		{th.Idle + time.Second, LivenessStale},
	} {
		s, _ := newFixture(now)
		s.LastActivity = TimePtr(now.Add(-tc.age))
		if got := s.Liveness(now, th); got != tc.want {
			t.Errorf("age %v: liveness = %v, want %v", tc.age, got, tc.want)
		}
	}

	// No timestamp and no hint: unknown, never "stale" — stale is a claim.
	s, _ := newFixture(now)
	s.LastActivity = nil
	if got := s.Liveness(now, th); got != LivenessUnknown {
		t.Errorf("no evidence: liveness = %v, want unknown", got)
	}
	if _, ok := s.Age(now); ok {
		t.Error("Age reported a duration with no LastActivity")
	}

	// A positive vendor signal overrides the age classification.
	s.LastActivity = TimePtr(now.Add(-3 * time.Hour))
	s.LivenessHint = LivenessPtr(LivenessLive)
	if got := s.Liveness(now, th); got != LivenessLive {
		t.Errorf("hint ignored: liveness = %v", got)
	}

	// Clock skew (a file mtime ahead of the local clock) clamps to zero age
	// rather than producing a negative duration.
	s.LivenessHint = nil
	s.LastActivity = TimePtr(now.Add(time.Hour))
	if d, ok := s.Age(now); !ok || d != 0 {
		t.Errorf("future LastActivity: age = %v (ok=%v), want 0", d, ok)
	}
}

// Presence is decided in exactly one place (Has), so renderers and Validate
// cannot disagree about what counts as data.
func TestPresenceSemantics(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	s, _ := newFixture(now)

	// A window with no usage figure does not make quota absent, and does not
	// make it zero either.
	w, ok := s.Window("seven_day")
	if !ok || w.UsedPercent != nil {
		t.Fatal("seven_day window should exist with a nil UsedPercent")
	}
	if !s.Has(FieldQuota) {
		t.Error("quota should be present via the five_hour window")
	}

	// Liveness counts as sourced only when the adapter supplied a hint.
	if s.Has(FieldLiveness) {
		t.Error("liveness should not be present without a hint")
	}

	empty := &Session{}
	if got := empty.Present(); !got.Empty() {
		t.Errorf("empty session reports present fields: %s", got)
	}
	if _, ok := empty.Model.Name(); ok {
		t.Error("nil Model reported a name")
	}

	// Unix 0 means "no value", not 1970.
	if UnixTimePtr(0) != nil || UnixTimePtr(-1) != nil {
		t.Error("non-positive unix seconds must decode as absent")
	}
}

// Field names are the stable identifiers used by fixtures and by third-party
// adapter documentation; ordinals are not.
func TestFieldNamesRoundTrip(t *testing.T) {
	for _, f := range AllFields {
		name := f.String()
		if name == "" || strings.HasPrefix(name, "field(") {
			t.Errorf("field %d has no stable name", uint8(f))
			continue
		}
		got, ok := ParseField(name)
		if !ok || got != f {
			t.Errorf("ParseField(%q) = %v, %v", name, got, ok)
		}
	}
	if _, ok := ParseField("not_a_field"); ok {
		t.Error("ParseField accepted an unknown name")
	}
}

// WorkspaceName is display-only string handling: it must find the basename of a
// Windows path even when the session was read from a fixture on another OS.
func TestWorkspaceNameHandlesBothSeparators(t *testing.T) {
	for _, tc := range []struct{ dir, want string }{
		{`C:\Users\dev\code\telltale`, "telltale"},
		{`C:\Users\dev\code\telltale\`, "telltale"},
		{"/home/dev/code/telltale", "telltale"},
		{"telltale", "telltale"},
	} {
		s := &Session{WorkspaceDir: Ptr(tc.dir)}
		got, ok := s.WorkspaceName()
		if !ok || got != tc.want {
			t.Errorf("WorkspaceName(%q) = %q, %v; want %q", tc.dir, got, ok, tc.want)
		}
	}
	for _, dir := range []string{"", `\`, "/"} {
		s := &Session{WorkspaceDir: Ptr(dir)}
		if got, ok := s.WorkspaceName(); ok {
			t.Errorf("WorkspaceName(%q) = %q, want absent", dir, got)
		}
	}
}

func TestCapabilitiesDisjointness(t *testing.T) {
	bad := Capabilities{
		Reported: NewFieldSet(FieldCost),
		Derived:  NewFieldSet(FieldCost),
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("a field declared both reported and derived was accepted")
	}
	if got := bad.Known(); !got.Has(FieldCost) {
		t.Errorf("Known() = %s", got)
	}
}
