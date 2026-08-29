package cursor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The fixture session uuids, from testdata/chats. All synthetic: this repo is
// public, and the real tree's manifests carry the operator's own working
// directories beside a store.db this reader must never open (docs/design.md
// §3.9's 2026-08-29 addendum).
//
// The tree deliberately reproduces every shape the 2026-08-29 survey measured,
// in the proportions it measured them:
//
//	…0001  a titled session, with the three neighbours this reader may not read
//	…0002  no title — 40 of 43 live manifests carry none
//	…0003  hasConversation:false, and nothing else in its directory: a shell
//	…0004  a schemaVersion this reader does not speak
//	…0005  a manifest that is not JSON at all
//	…0006  no cwd and no title, and no hasConversation key either
//	…0007  an updatedAtMs past the future-skew guard
//	…0008  no updatedAtMs key at all
//	…0009  a session directory holding store.db and no manifest — 22 of the 65
//	       live session directories are this
const (
	cli1 = "00000000-cccc-4fff-8aaa-000000000001"
	cli2 = "00000000-cccc-4fff-8aaa-000000000002"
	cli3 = "00000000-cccc-4fff-8aaa-000000000003"
	cli4 = "00000000-cccc-4fff-8aaa-000000000004"
	cli5 = "00000000-cccc-4fff-8aaa-000000000005"
	cli6 = "00000000-cccc-4fff-8aaa-000000000006"
	cli7 = "00000000-cccc-4fff-8aaa-000000000007"
	cli8 = "00000000-cccc-4fff-8aaa-000000000008"
	cli9 = "00000000-cccc-4fff-8aaa-000000000009"
)

// chatsMtimes is the manifest mtime each fixture is stamped with.
//
// They are set by the test rather than checked in, and they have to be: a git
// checkout stamps every file with the moment it landed on disk, so a fixture
// whose expected age came from its own mtime would assert "now" and pass
// against any code at all. Stamping them here is also what lets the Q8 fold be
// exercised in both directions — …0001's mtime AGREES with its updatedAtMs the
// way all 43 live manifests do, and …0002's is 90 s past it, which is the case
// the fold exists for.
func chatsMtimes(now time.Time) map[string]time.Time {
	return map[string]time.Time{
		cli1: msAt(100), // == updatedAtMs
		cli2: msAt(290), // 90s past updatedAtMs; the fold takes the mtime
		cli3: msAt(300),
		cli4: msAt(500),
		cli5: msAt(500),
		cli6: msAt(400), // == updatedAtMs
		cli7: msAt(600), // updatedAtMs is year 2100 and is refused
		cli8: now.Add(time.Hour),
		cli9: msAt(100),
	}
}

// chatsRoot copies testdata/chats into a temp tree and stamps the manifests.
func chatsRoot(t *testing.T) string {
	t.Helper()
	return chatsRootPatched(t, nil)
}

// chatsRootPatched is chatsRoot with a byte-for-byte rewrite applied to every
// manifest on the way through.
//
// The renames must be length-preserving for the same reason driftedRoot's are:
// what makes a drift fixture a drift fixture is that it is the verified corpus
// with one identifier moved and nothing else.
func chatsRootPatched(t *testing.T, renames map[string]string) string {
	t.Helper()
	for from, to := range renames {
		if len(from) != len(to) {
			t.Fatalf("rename %q -> %q changes length; the patch would change the fixture's shape too", from, to)
		}
	}

	src := filepath.Join("testdata", chatsDir)
	dst := filepath.Join(t.TempDir(), chatsDir)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	stamps := chatsMtimes(now)
	err := filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != metaFile {
			return err
		}
		if len(renames) > 0 {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for from, to := range renames {
				raw = []byte(strings.ReplaceAll(string(raw), from, to))
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				return err
			}
		}
		id := filepath.Base(filepath.Dir(path))
		stamp, ok := stamps[id]
		if !ok {
			t.Fatalf("fixture %s has no mtime in chatsMtimes; the table and the tree have drifted apart", id)
		}
		return os.Chtimes(path, stamp, stamp)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// cliAdapter is an adapter reading the CLI fixtures and no Composer store.
func cliAdapter(t *testing.T) *Adapter {
	t.Helper()
	return NewWithRoots("", chatsRoot(t))
}

func readCLIOne(t *testing.T, a *Adapter, id string) *model.Session {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, r := range refs {
		if r.ID != cliIDPrefix+id {
			continue
		}
		s, err := a.Read(context.Background(), r)
		if err != nil {
			t.Fatalf("Read %s: %v", id, err)
		}
		return s
	}
	t.Fatalf("Discover did not list %s; it listed %v", id, refs)
	return nil
}

func cliIDs(t *testing.T, a *Adapter) []string {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var out []string
	for _, r := range refs {
		if id, ok := strings.CutPrefix(r.ID, cliIDPrefix); ok {
			out = append(out, id)
		}
	}
	return out
}

// The gap §3.9 recorded on 2026-08-17 — a live cursor-agent CLI session drawing
// no HUD row — is closed, and the filter that closes it drops exactly the four
// shapes that are not sessions.
func TestCLISessionsDrawRows(t *testing.T) {
	got := cliIDs(t, cliAdapter(t))
	want := []string{cli1, cli2, cli6, cli7, cli8}
	if len(got) != len(want) {
		t.Fatalf("discovered %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discovered %v, want %v (sorted by id)", got, want)
		}
	}
}

// hasConversation:false is an empty shell, and the ruling is measured rather
// than assumed. All three such manifests on the survey machine held nothing but
// themselves and stood 263–387 ms past their own createdAtMs.
func TestAnEmptyShellIsNotASession(t *testing.T) {
	for _, id := range cliIDs(t, cliAdapter(t)) {
		if id == cli3 {
			t.Fatal("a manifest with hasConversation:false drew a row")
		}
	}
}

// An ABSENT hasConversation is not a declared false. A vendor that stopped
// writing the key must not empty the HUD.
func TestAnAbsentConversationFlagKeepsItsRow(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli6)
	if s.LastActivity == nil {
		t.Error("the row lost its clock")
	}
}

// A directory holding store.db and no manifest draws nothing. It is the
// commonest shape in the live tree — 22 of 65 — and it carries no readable
// name, workspace or vendor timestamp, so a row for one would date a session
// from a directory mtime whose meaning was never measured.
func TestASessionDirectoryWithNoManifestDrawsNothing(t *testing.T) {
	for _, id := range cliIDs(t, cliAdapter(t)) {
		if id == cli9 {
			t.Fatal("a session directory with no meta.json drew a row")
		}
	}
}

func TestTheVendorTitleIsTheName(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli1)
	if s.Name == nil || *s.Name != "Synthetic Session Title" {
		t.Errorf("name = %v, want the manifest's own title", s.Name)
	}
	if s.WorkspaceDir == nil || *s.WorkspaceDir != `C:\synthetic\alpha-project` {
		t.Errorf("workspace = %v, want the manifest's cwd verbatim", s.WorkspaceDir)
	}
}

// 40 of 43 live manifests carry no title, so the fallback is the ordinary case
// rather than the edge. It is the workspace basename — the fallback
// internal/hud's own sessionLabel applies and internal/adapter/pi applies at
// the adapter — and never a fabricated label.
func TestAnAbsentTitleFallsBackToTheWorkspaceBasename(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli2)
	if s.Name == nil || *s.Name != "beta-project" {
		t.Errorf("name = %v, want the cwd's basename", s.Name)
	}
}

// No title and no cwd leaves the name ABSENT rather than invented. The HUD's
// sessionLabel then falls through to the session id, which is the last thing
// that is actually known.
func TestNoTitleAndNoWorkspaceLeavesTheNameAbsent(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli6)
	if s.Name != nil {
		t.Errorf("name = %q, want absent: the manifest carried neither a title nor a cwd", *s.Name)
	}
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want absent", *s.WorkspaceDir)
	}
}

// The Q8 fold, in both directions. The Composer store deliberately does NOT
// fold its file mtime because one file backs every session there; here every
// session has its own manifest, so the mtime dates the session and the fold is
// the shape every other adapter uses.
func TestLastActivityFoldsTheManifestMtime(t *testing.T) {
	a := cliAdapter(t)

	agreeing := readCLIOne(t, a, cli1)
	if agreeing.LastActivity == nil || !agreeing.LastActivity.Equal(msAt(100)) {
		t.Errorf("last_activity = %v, want the vendor's own updatedAtMs", agreeing.LastActivity)
	}

	folded := readCLIOne(t, a, cli2)
	if folded.LastActivity == nil || !folded.LastActivity.Equal(msAt(290)) {
		t.Errorf("last_activity = %v, want the newer mtime", folded.LastActivity)
	}
}

// A timestamp past the future-skew guard is refused, and the row falls back to
// the reading that is left. Never a negative age, never "0s".
func TestAFutureUpdatedAtIsRefusedNotClamped(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli7)
	if s.LastActivity == nil || !s.LastActivity.Equal(msAt(600)) {
		t.Errorf("last_activity = %v, want the manifest's mtime; the vendor timestamp is in 2100", s.LastActivity)
	}
}

// No clock anywhere degrades the field and says so. Absence with a reason, not
// a plausible number.
func TestNoReadableClockDegradesTheAge(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli8)
	if s.LastActivity != nil {
		t.Errorf("last_activity = %v, want absent", s.LastActivity)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Errorf("degraded = %s, want last_activity", s.Degraded)
	}
	if !hasNote(s, "manifest") {
		t.Errorf("diagnostics = %v, want one naming the manifest", s.Diagnostics)
	}
}

// A schemaVersion this reader does not speak draws no row — dropfile's rule,
// for dropfile's reason — and the skip is SAID. A silent skip would turn a
// vendor format bump into "you have no CLI sessions", which is a wrong answer
// rather than a missing one.
func TestAnUnknownSchemaVersionIsSkippedAndSaidOutLoud(t *testing.T) {
	a := cliAdapter(t)
	for _, id := range cliIDs(t, a) {
		if id == cli4 {
			t.Fatal("a manifest with an unrecognized schemaVersion drew a row")
		}
	}
	s := readCLIOne(t, a, cli1)
	if !hasNote(s, "schemaVersion this adapter does not read") {
		t.Errorf("diagnostics = %v, want the unknown-schemaVersion note", s.Diagnostics)
	}
}

// A manifest that is not JSON degrades to its own row going missing, with a
// count. Structure only — the note never quotes the file.
func TestAnUnparseableManifestIsCountedNotQuoted(t *testing.T) {
	s := readCLIOne(t, cliAdapter(t), cli1)
	if !hasNote(s, "did not parse") {
		t.Errorf("diagnostics = %v, want the unparseable-manifest note", s.Diagnostics)
	}
}

// Every row says which of Cursor's two stores it was read from, and the labels
// are symmetric: an IDE row carries one too, so that the ABSENCE of a label is
// never the thing that identifies a store.
func TestEveryRowNamesTheStoreItCameFrom(t *testing.T) {
	a := NewWithRoots(root(), chatsRoot(t))
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	ide, cli := 0, 0
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatalf("Read %s: %v", ref.ID, err)
		}
		src := extraValue(s, extraSource)
		switch {
		case strings.HasPrefix(ref.ID, cliIDPrefix):
			cli++
			if src != sourceCLI {
				t.Errorf("%s: source = %q, want %q", ref.ID, src, sourceCLI)
			}
			if extraValue(s, extraNotInManifest) == "" {
				t.Errorf("%s: a CLI row does not say which fields its store lacks", ref.ID)
			}
		default:
			ide++
			if src != sourceIDE {
				t.Errorf("%s: source = %q, want %q", ref.ID, src, sourceIDE)
			}
		}
		if err := s.Validate(a.Capabilities()); err != nil {
			t.Errorf("%s: Validate: %v", ref.ID, err)
		}
	}
	if ide == 0 || cli == 0 {
		t.Fatalf("both stores must render: ide=%d cli=%d", ide, cli)
	}
}

// The id prefix routes the ref. A bare Composer id must never be answered out
// of the CLI tree, and vice versa — a fallback would answer "which store is
// this row from" with "whichever one still had it".
func TestTheIDPrefixRoutesTheRead(t *testing.T) {
	a := NewWithRoots(root(), chatsRoot(t))

	_, err := a.Read(context.Background(), model.SessionRef{Vendor: Vendor, ID: cli1})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Errorf("an unprefixed CLI id was answered out of the Composer store: err = %v, want ErrSessionGone", err)
	}
	_, err = a.Read(context.Background(), model.SessionRef{Vendor: Vendor, ID: cliIDPrefix + idHappy})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Errorf("a prefixed Composer id was answered out of the CLI tree: err = %v, want ErrSessionGone", err)
	}
}

// Nothing beside the manifest is ever opened. The reader touches one file name
// and the neighbours hold planted markers standing in for the real thing: the
// prompt history, and the store.db that the credential rule and §3.9's
// build cautions both put out of bounds.
func TestNothingBesideTheManifestIsRead(t *testing.T) {
	a := cliAdapter(t)
	for _, id := range cliIDs(t, a) {
		s := readCLIOne(t, a, id)
		for _, marker := range []string{promptMarker, credentialMarker} {
			for _, field := range sessionStrings(s) {
				if strings.Contains(field, marker) {
					t.Fatalf("%s: %q reached a displayed field: %q", id, marker, field)
				}
			}
		}
	}
}

// A rename of the manifest's clock key is the silent failure this vendor's CLI
// half has, and it is silent in a sharper way than the Composer half's: the
// mtime still supplies an age, so nothing degrades and nothing looks wrong.
// The row's age has quietly stopped being the vendor's reading, and only the
// canary says so.
func TestARenamedManifestClockSaysSo(t *testing.T) {
	a := NewWithRoots("", chatsRootPatched(t, map[string]string{"updatedAtMs": "refreshedAt"}))
	s := readCLIOne(t, a, cli1)

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a tree with no manifest clocks reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, canaryChatsClock.Name) || !strings.Contains(report, chatsVerifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	// The age survived, out of the mtime, so it must NOT be marked degraded —
	// a value that was read is a value (drift.Watch.Fold).
	if s.LastActivity == nil {
		t.Error("the row lost its age; the mtime is still readable")
	}
	if s.Degraded.Has(model.FieldLastActivity) {
		t.Error("last_activity is degraded although it was sourced")
	}
}

// The verified tree is silent.
func TestTheVerifiedManifestTreeReportsNoDrift(t *testing.T) {
	a := cliAdapter(t)
	for _, id := range cliIDs(t, a) {
		if d := driftReport(readCLIOne(t, a, id)); d != "" {
			t.Errorf("%s: the verified tree reported drift: %q", id, d)
		}
	}
}

// Absence of one store is ordinary and costs only its own rows. Absence of both
// is the vendor being absent.
func TestEitherStoreAloneStillRenders(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tree")

	if ids := cliIDs(t, NewWithRoots("", chatsRoot(t))); len(ids) == 0 {
		t.Error("a machine with no Cursor IDE store drew no CLI rows")
	}
	if refs, err := NewWithRoots(root(), missing).Discover(context.Background()); err != nil || len(refs) == 0 {
		t.Errorf("a machine with no CLI tree drew no Composer rows: refs=%d err=%v", len(refs), err)
	}
	if _, err := NewWithRoots(missing, missing).Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Errorf("both stores absent: err = %v, want ErrVendorAbsent", err)
	}
}

// A Composer store that EXISTS and cannot be read wins outright. Returning CLI
// rows beside it would draw a Cursor section that looks like a complete answer
// while the larger store is silently unreadable.
func TestAnUnreadableComposerStoreStillWinsOverCLIRows(t *testing.T) {
	a := NewWithRoots(driftedRoot(t, map[string]string{tableHeaders: "composerHeaderX"}), chatsRoot(t))
	if _, err := a.Discover(context.Background()); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("Discover err = %v, want ErrSchemaMismatch", err)
	}
}

// ---------------------------------------------------------------- helpers

func hasNote(s *model.Session, want string) bool {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, want) {
			return true
		}
	}
	return false
}

func extraValue(s *model.Session, label string) string {
	for _, e := range s.Extras {
		if e.Label == label {
			return e.Value
		}
	}
	return ""
}

// sessionStrings is every string a Session can put on screen.
func sessionStrings(s *model.Session) []string {
	out := []string{s.ID}
	if s.Name != nil {
		out = append(out, *s.Name)
	}
	if s.WorkspaceDir != nil {
		out = append(out, *s.WorkspaceDir)
	}
	if s.Model != nil {
		out = append(out, s.Model.ID, s.Model.DisplayName)
	}
	for _, e := range s.Extras {
		out = append(out, e.Label, e.Value)
	}
	out = append(out, s.Diagnostics...)
	return out
}
