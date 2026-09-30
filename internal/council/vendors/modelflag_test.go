package vendors

import (
	"errors"
	"reflect"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestWithModelPlacesTheFlagWhereEachCLIParsesIt pins the argv of every seat
// shape that council spawns, with a model request on it. The expected argv is
// written out in full, so a placement that moves is a failure that names the
// new position.
func TestWithModelPlacesTheFlagWhereEachCLIParsesIt(t *testing.T) {
	const ws = `C:\ws`
	claudeFirst, _ := Claude{}.FirstTurn("brief", ws, "claude.exe", PostureRead)
	codexExec, _ := Codex{}.FirstTurn("brief", ws, "codex.exe", PostureRead)
	codexResume, _ := Codex{}.NextTurn("brief", ws, "codex.exe", "thread-1", PostureRead)
	agyFirst, _ := Antigravity{}.FirstTurn("brief", ws, "agy.exe", PostureRead)
	grokBatch, _ := Grok{}.FirstTurn("brief", ws, "grok.exe", PostureRead)

	cases := []struct {
		name string
		spec runner.Spec
		want []string
	}{
		{"claude first turn", claudeFirst,
			append([]string{"--model", "opus"}, claudeFirst.Args...)},
		{"codex exec keeps - last", codexExec,
			append(append(append([]string{}, codexExec.Args[:len(codexExec.Args)-1]...),
				"-c", `model="gpt-5.6-sol"`), "-")},
		{"codex exec resume keeps - last", codexResume,
			append(append(append([]string{}, codexResume.Args[:len(codexResume.Args)-1]...),
				"-c", `model="gpt-5.6-sol"`), "-")},
		{"codex app-server", runner.Spec{Vendor: model.VendorCodex, Args: []string{"app-server"}},
			[]string{"app-server", "-c", `model="gpt-5.6-sol"`}},
		{"agy before -p", agyFirst,
			append([]string{"--model", "gemini-3-pro"}, agyFirst.Args...)},
		{"grok agent stdio", runner.Spec{Vendor: model.VendorGrok, Args: []string{"agent", "stdio"}},
			[]string{"agent", "--model", "grok-4.5", "stdio"}},
		{"grok batch", grokBatch,
			append([]string{"--model", "grok-4.5"}, grokBatch.Args...)},
	}
	req := map[model.VendorID]string{
		model.VendorClaude:      "opus",
		model.VendorCodex:       "gpt-5.6-sol",
		model.VendorAntigravity: "gemini-3-pro",
		model.VendorGrok:        "grok-4.5",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.spec.Args...)
			got, err := WithModel(tc.spec, req[tc.spec.Vendor])
			if err != nil {
				t.Fatalf("WithModel: %v", err)
			}
			if !reflect.DeepEqual(got.Args, tc.want) {
				t.Fatalf("argv\n got %q\nwant %q", got.Args, tc.want)
			}
			if !reflect.DeepEqual(tc.spec.Args, before) {
				t.Fatalf("WithModel changed the caller's argv: %q", tc.spec.Args)
			}
		})
	}
}

// TestWithModelEmptyRequestIsTheVendorDefault pins the absent case: no
// request, no flag, and the argv is byte for byte what it was before --model
// existed.
func TestWithModelEmptyRequestIsTheVendorDefault(t *testing.T) {
	spec, _ := Claude{}.FirstTurn("brief", "ws", "claude.exe", PostureRead)
	got, err := WithModel(spec, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, spec.Args) {
		t.Fatalf("an empty request changed argv: %q", got.Args)
	}
}

// TestWithModelRefusesAnUnmeasuredSeat pins the refusal. The Cursor seat has no
// measured flag, so a request for it is an error and never a spec that runs
// without the request.
func TestWithModelRefusesAnUnmeasuredSeat(t *testing.T) {
	spec := runner.Spec{Vendor: model.VendorCursor, Args: []string{"acp"}}
	got, err := WithModel(spec, "composer-2")
	if !errors.Is(err, ErrModelNotSupported) {
		t.Fatalf("err = %v, want ErrModelNotSupported", err)
	}
	if got.Binary != "" || got.Args != nil {
		t.Fatalf("a refused request returned a runnable spec: %+v", got)
	}
	if _, err := ModelFlagFor(model.VendorCursor); !errors.Is(err, ErrModelNotSupported) {
		t.Fatalf("ModelFlagFor(cursor) = %v, want ErrModelNotSupported", err)
	}
}

// TestValidModelName pins the argv safety rule in both directions.
func TestValidModelName(t *testing.T) {
	for _, ok := range []string{"opus", "claude-fable-5", "o3", "gpt-5.6-sol", "grok-4.5", "vendor/model:tag", "a@b+c_d"} {
		if err := ValidModelName(ok); err != nil {
			t.Errorf("ValidModelName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-opus", "--help", "a b", `a"b`, "a&b", "a|b", "a%b", "a^b", "a\nb"} {
		if err := ValidModelName(bad); !errors.Is(err, ErrModelName) {
			t.Errorf("ValidModelName(%q) = %v, want ErrModelName", bad, err)
		}
	}
}

// TestEveryMeasuredModelFlagHasAPlacement pins that the table and the switch in
// WithModel name the same seats. A seat added to the table with no placement
// would be refused at spawn, which is a room that says yes at the door and no
// at the first brief.
func TestEveryMeasuredModelFlagHasAPlacement(t *testing.T) {
	for id, f := range modelFlags {
		if f.Help == "" || f.MeasuredAt == "" || f.Spelling == "" {
			t.Errorf("%s: a measured flag must quote its help line and name its build", id)
		}
		if _, err := WithModel(runner.Spec{Vendor: id, Args: []string{"x"}}, "m"); err != nil {
			t.Errorf("%s: measured, but WithModel has no placement: %v", id, err)
		}
	}
}
