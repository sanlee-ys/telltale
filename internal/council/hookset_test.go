package council

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// settingsFixture writes a settings file and returns its path.
func settingsFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// realShape is the shape of the file this reads from: a hooks section beside
// things that must never travel with it.
const realShape = `{
  "permissions": {"allow": ["Bash(mkdir:*)", "Bash(git status:*)"], "ask": ["Bash(git push:*)"]},
  "env": {"SOME_TOKEN": "sk-not-a-real-secret-but-treat-it-as-one"},
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "python3 $HOME/.claude/hooks/credential-guard.py"}]}
    ]
  }
}`

// TestOnlyHooksAreCopied is the safety test of this file, and it is not
// belt-and-braces.
//
// Measured in the spike: a --settings file carrying
// {"permissions":{"allow":["Bash(mkdir:*)"]}} made `mkdir zzz-leak-probe` run
// with NO permission request and the directory landed on disk. So a permissions
// block reaching this file re-admits exactly the allow rules the gated posture
// passes --setting-sources "" to drop, and the room would go on rendering
// `gated` while calls walked straight past the gate.
func TestOnlyHooksAreCopied(t *testing.T) {
	hs := loadHookSetFrom(settingsFixture(t, realShape))
	defer hs.Cleanup()

	if !hs.Wired() {
		t.Fatalf("nothing was wired from a settings file that has hooks: %q", hs.Absent)
	}
	body, err := os.ReadFile(hs.Path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the file written is not valid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the file has %d top-level keys, want exactly one: %v", len(got), keysOf(got))
	}
	if _, ok := got["hooks"]; !ok {
		t.Fatalf("the one key is not hooks: %v", keysOf(got))
	}
	// Asserted on the raw bytes as well as on the parse. The parse proves the
	// structure; this proves nothing rode along inside a string.
	for _, banned := range []string{"permissions", "allow", "SOME_TOKEN", "sk-not-a-real-secret"} {
		if strings.Contains(string(body), banned) {
			t.Errorf("%q reached the hooks file: %s", banned, body)
		}
	}
}

// TestHookConfigSurvivesVerbatim. A hook command is a shell string the user
// wrote, and a round trip that dropped or renamed a field would produce a guard
// that silently does something else.
func TestHookConfigSurvivesVerbatim(t *testing.T) {
	hs := loadHookSetFrom(settingsFixture(t, realShape))
	defer hs.Cleanup()

	body, err := os.ReadFile(hs.Path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hooks.PreToolUse) != 1 || len(got.Hooks.PreToolUse[0].Hooks) != 1 {
		t.Fatalf("the hook did not survive the copy: %s", body)
	}
	h := got.Hooks.PreToolUse[0]
	if h.Matcher != "Bash" {
		t.Errorf("matcher = %q, want Bash", h.Matcher)
	}
	if want := "python3 $HOME/.claude/hooks/credential-guard.py"; h.Hooks[0].Command != want {
		t.Errorf("command = %q, want %q", h.Hooks[0].Command, want)
	}
}

// TestNothingToCopyWiresNothing. Four ways to have no hooks, and every one of
// them must produce a seat that SAYS it carries none rather than one that
// claims a guard it does not have.
func TestNothingToCopyWiresNothing(t *testing.T) {
	cases := map[string]string{
		"no hooks section":   `{"permissions": {"allow": ["Bash(mkdir:*)"]}}`,
		"empty hooks":        `{"hooks": {}}`,
		"hooks is null":      `{"hooks": null}`,
		"hooks is not a map": `{"hooks": "please run my hooks"}`,
		"malformed json":     `{"hooks": {"PreToolUse": [`,
		"not json at all":    "# a comment, which JSON does not have\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			hs := loadHookSetFrom(settingsFixture(t, body))
			defer hs.Cleanup()
			if hs.Wired() {
				t.Fatalf("wired a hooks file from %s: %q", name, body)
			}
			if hs.Absent == "" {
				t.Error("nothing was wired and the column has no words for why")
			}
		})
	}
}

// TestMissingSettingsFileWiresNothing. The room must open on a machine that has
// never written a settings file, and it must not claim a guard there.
func TestMissingSettingsFileWiresNothing(t *testing.T) {
	hs := loadHookSetFrom(filepath.Join(t.TempDir(), "nothing-here.json"))
	defer hs.Cleanup()
	if hs.Wired() {
		t.Fatal("wired a hooks file from a settings file that does not exist")
	}
	if hs.Absent == "" {
		t.Error("the column has no words for a missing settings file")
	}
}

// TestThePathIsAbsolute. Measured, and it cost a probe: --settings resolves a
// relative path against the CHILD's working directory, which is the workspace
// council was pointed at rather than telltale's own. A relative path came back
// as "Error: Settings file not found" and the seat exited 1.
func TestThePathIsAbsolute(t *testing.T) {
	hs := loadHookSetFrom(settingsFixture(t, realShape))
	defer hs.Cleanup()
	if !filepath.IsAbs(hs.Path) {
		t.Errorf("Path = %q, want an absolute path: the child resolves it against the workspace", hs.Path)
	}
}

// TestCleanupRemovesTheFile. The file is a copy of part of the user's private
// configuration; leaving one behind per room would accumulate them in the temp
// directory with nothing to say where they came from.
func TestCleanupRemovesTheFile(t *testing.T) {
	hs := loadHookSetFrom(settingsFixture(t, realShape))
	path := hs.Path
	hs.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the hooks file survived cleanup: stat err = %v", err)
	}
	// Called twice on the way out of a room — teardown and Run's defer — so the
	// second call must not panic or report anything.
	hs.Cleanup()
	// And a zero HookSet is the normal state of a read-only room.
	HookSet{}.Cleanup()
}

// TestTheBadgeTracksTheFileAndNotTheIntent.
//
// The whole point of deriving the claim from hs.Wired() is that an unreadable
// settings file, an empty hooks section and a temp directory that could not be
// created all end in the same place: no guard. A badge keyed off "we tried"
// would survive all three.
func TestTheBadgeTracksTheFileAndNotTheIntent(t *testing.T) {
	wired := postureClaim(model.VendorClaude, true, true, true, true)
	absent := postureClaim(model.VendorClaude, true, true, true, false)

	if wired.Level != SandboxGated || absent.Level != SandboxGated {
		t.Fatal("the gate's badge changed level over whether a hook is behind it; the seat still asks either way")
	}
	if wired.Detail == absent.Detail {
		t.Fatal("a seat with the guard wired and one without say the same thing")
	}
	if !strings.Contains(wired.Detail, "carried into this seat and do run") {
		t.Errorf("the wired detail does not state that the hooks run: %q", wired.Detail)
	}
	if !strings.Contains(absent.Detail, "No hooks were carried into this seat") {
		t.Errorf("the absent detail does not admit the guard is missing: %q", absent.Detail)
	}
	// The claim that must never appear without a file behind it.
	if strings.Contains(absent.Detail, "do run in front of it") {
		t.Errorf("a seat with no hooks claims they run: %q", absent.Detail)
	}
	// Both branches keep the carve-out that makes the guard worth wiring at all.
	for _, d := range []string{wired.Detail, absent.Detail} {
		if !strings.Contains(d, "read-only are approved without asking") {
			t.Errorf("the detail dropped the read-only carve-out: %q", d)
		}
		if !strings.Contains(d, "allow rules are dropped") {
			t.Errorf("the detail stopped saying the allow rules are dropped: %q", d)
		}
	}
}

// TestOnlyAGatedRoomCopiesHooks pins the condition in Run against the one in
// seatPosture. They are the same condition written twice, and the failure mode
// of them drifting is a temp file written for a room that never uses it — or,
// the other way, a gated seat launched with no hooks file at all.
func TestOnlyAGatedRoomCopiesHooks(t *testing.T) {
	if wantsHooks(Options{}) {
		t.Error("a read-only room copied the user's hooks; it loads them natively")
	}
	if wantsHooks(Options{Write: true, Auto: true}) {
		t.Error("--write --auto copied the user's hooks; it loads them natively, so each would fire twice")
	}
	if !wantsHooks(Options{Write: true}) {
		t.Error("the gated room did not copy the user's hooks, which is the seat that has none otherwise")
	}
}

// TestHooksPathNeverReachesState is the same privacy boundary the brief has, and
// it is the same rule rather than a resemblance: the path names a file copied
// out of the user's private configuration, so only the boolean crosses.
func TestHooksPathNeverReachesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks-tell-tale-marker.json")
	m := newWithBrief(Options{Write: true}, Brief{}, HookSet{Path: path}, Reattachment{})
	m.st.Width, m.st.Height = 120, 24

	if strings.Contains(Render(m.st, PlainStyles(), GlyphsFor(false)), "hooks-tell-tale-marker") {
		t.Error("the hooks file path was rendered to the screen")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
