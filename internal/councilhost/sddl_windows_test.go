//go:build windows

package councilhost

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Identities are compared as SIDs here, never as substrings of an SDDL string,
// and that rule was learned twice at a cost.
//
//   - Looking for Everyone as `S-1-1-0` came back CLEAN while Everyone was on
//     the pipe: Windows had rendered it as the alias `WD`.
//   - Looking for the current user's literal SID FAILED on CI, where the runner
//     is the built-in Administrator (RID 500) and Windows renders that account
//     as `LA`. The descriptor had applied perfectly; the assertion was comparing
//     spellings. Measured 2026-09-01 on windows-latest:
//     `D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;LA)`.
//
// Both are one defect. An SDDL string is a RENDERING, and which spelling an
// identity gets is the API's choice rather than the object's state. The first
// failure is the dangerous direction — it reports a pipe clean that is not — so
// readPipeDACLSIDs and windows.EqualSid do the work, and the strings below are
// only ever logged for a human to read.

// testPipeName gives each test its own pipe, so a leftover instance from a
// failed run cannot make the next one pass or fail for the wrong reason.
func testPipeName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\telltale-councilhost-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
}

// TestTheDefaultDescriptorIsTheLeakWeRefuse measures the claim design.md §7.28
// makes about the default, instead of citing it.
//
// Microsoft's *Named Pipe Security and Access Rights* page says a pipe created
// with a NULL lpSecurityAttributes grants read access to Everyone and to the
// anonymous account. This repo does not take a behaviour claim off a
// documentation page (ADR-001, design.md §4a.1), so this creates exactly such a
// pipe and reads its DACL back.
//
// It is the NEGATIVE CONTROL for the test below, and without it that test is
// much weaker than it looks: on its own,
// TestThePipeCarriesTheExplicitDescriptor proves only that a descriptor was
// applied. This is what proves the descriptor PREVENTS something — that the
// default really would have put the operator's conversation in front of every
// local account.
//
// A skip rather than a failure if the platform ever stops doing this: the point
// is to record what this machine does, and a future Windows that tightened the
// default would make the finding obsolete rather than make telltale wrong.
func TestTheDefaultDescriptorIsTheLeakWeRefuse(t *testing.T) {
	name := testPipeName(t)
	namep, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(namep,
		windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1, 4096, 4096, 0,
		nil, // the default descriptor — the thing being measured
	)
	if err != nil {
		t.Fatalf("could not create a default-descriptor pipe: %v", err)
	}
	defer windows.CloseHandle(h)

	shown, err := readPipeDACL(h)
	if err != nil {
		t.Fatalf("could not read the default pipe's DACL: %v", err)
	}
	t.Logf("measured default named-pipe DACL on this machine: %s", shown)

	sids, err := readPipeDACLSIDs(h)
	if err != nil {
		t.Fatalf("could not walk the default pipe's DACL: %v", err)
	}
	everyone, err := daclAdmits(sids, windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	if !everyone {
		t.Skipf("this Windows build's default named-pipe DACL does not admit Everyone (%s) — "+
			"design.md §7.28's claim about the default was MEASURED on a build that did, and "+
			"a build that tightened it should be re-measured rather than assumed", shown)
	}
	if anon, err := daclAdmits(sids, windows.WinAnonymousSid); err == nil && !anon {
		t.Logf("this build's default admits Everyone but NOT the anonymous account (%s). "+
			"§7.28 names both; the Everyone half is what the refusal rests on.", shown)
	}
}

// TestThePipeCarriesTheExplicitDescriptor is the positive half.
//
// It asserts what the OBJECT carries, not what was requested. Passing a
// SecurityAttributes proves an intention; walking the DACL off the live object
// proves the pipe. The difference matters because a descriptor that failed to
// apply would leave the default in place, which is precisely the leak measured
// above.
//
// Three assertions, each a separate thing that could go wrong:
//
//  1. The three intended identities are present, so the pipe is usable by the
//     host, by an administrator debugging it, and by LocalSystem.
//  2. Everyone is ABSENT. This is the one that matters. A pipe carrying agent
//     transcript content must not be readable by every local account.
//  3. ANONYMOUS LOGON is absent, for the same reason one step further out.
func TestThePipeCarriesTheExplicitDescriptor(t *testing.T) {
	name := testPipeName(t)
	ln, err := Listen(name)
	if err != nil {
		t.Fatalf("could not create the pipe: %v", err)
	}
	defer ln.Close()

	mine, err := currentUserSID()
	if err != nil {
		t.Fatalf("could not read this process's own SID: %v", err)
	}

	// Opened BY NAME rather than through the listener's own handle, so the
	// claim is about what the object presents to any other process — which is
	// the property that matters — and not about the creator's private view.
	h, err := openPipeForRead(name)
	if err != nil {
		t.Fatalf("could not open the pipe to read its descriptor: %v", err)
	}
	defer windows.CloseHandle(h)

	shown, err := readPipeDACL(h)
	if err != nil {
		t.Fatal(err)
	}
	sids, err := readPipeDACLSIDs(h)
	if err != nil {
		t.Fatalf("could not walk the pipe's DACL: %v", err)
	}
	t.Logf("applied SDDL: %s", ln.SDDL())
	t.Logf("pipe DACL as the object carries it: %s", shown)

	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		name string
		sid  *windows.SID
	}{
		{"LocalSystem", system},
		{"Administrators", admins},
		{"this process's own user", mine},
	} {
		if !daclAdmitsSID(sids, want.sid) {
			t.Errorf("the pipe's DACL does not admit %s (%s) — a descriptor that failed to "+
				"apply leaves the DEFAULT in place, which is the leak this test's negative "+
				"control measures.\n  DACL: %s", want.name, want.sid.String(), shown)
		}
	}

	everyone, err := daclAdmits(sids, windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	if everyone {
		t.Fatalf("the pipe admits Everyone. This pipe carries agent transcript content and a "+
			"dispatch channel, so that is every local account able to read the operator's "+
			"conversation — the exact shape of mistake design.md §7.24 was written about.\n"+
			"  DACL: %s", shown)
	}
	anon, err := daclAdmits(sids, windows.WinAnonymousSid)
	if err != nil {
		t.Fatal(err)
	}
	if anon {
		t.Fatalf("the pipe admits ANONYMOUS LOGON.\n  DACL: %s", shown)
	}

	if !strings.HasPrefix(ln.SDDL(), "D:P") {
		t.Errorf("the descriptor is not a PROTECTED DACL, so an inherited entry could "+
			"widen it: %s", ln.SDDL())
	}
	// Three entries and no more. A fourth would be an identity nobody argued
	// for, and the argument is the whole of §7.28's descriptor section.
	//
	// On a machine where the current user IS the built-in Administrator — a CI
	// runner, for one — the three requested entries can collapse to fewer,
	// because the account and a well-known group are then the same SID. Two is
	// therefore legal and four is not.
	if len(sids) < 2 || len(sids) > 3 {
		t.Errorf("the pipe's DACL carries %d entries, expected 2 or 3 (SYSTEM, "+
			"Administrators, this user — which can coincide).\n  DACL: %s", len(sids), shown)
	}
}

// TestTheDescriptorNamesThisUserLiterally pins the OW ruling.
//
// design.md §7.28 rules the literal SID over CREATOR OWNER, because OW is a
// placeholder an object substitutes at creation and whether a named pipe
// substitutes it the way a file does was never measured. A later session
// tidying the string to OW would reopen a question this design declined to
// depend on, so the refusal is pinned rather than left in prose.
func TestTheDescriptorNamesThisUserLiterally(t *testing.T) {
	sddl, err := ownerOnlySDDL()
	if err != nil {
		t.Fatal(err)
	}
	mine, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sddl, mine.String()) {
		t.Fatalf("the descriptor does not carry this user's literal SID %s: %s", mine.String(), sddl)
	}
	if strings.Contains(sddl, ";OW)") {
		t.Fatalf("the descriptor uses CREATOR OWNER. §7.28 declined to depend on OW "+
			"substituting for a named-pipe DACL, because that was never measured: %s", sddl)
	}
}

// TestAPipeNameCannotBeTakenTwice pins the anti-squatting half the server owns.
//
// FILE_FLAG_FIRST_PIPE_INSTANCE is set on every create, so a name another
// process already holds fails OUTRIGHT rather than adding an instance to
// somebody else's pipe. The client half is Dial's peer check, and both exist
// because the window between one instance closing and the next opening is real.
func TestAPipeNameCannotBeTakenTwice(t *testing.T) {
	name := testPipeName(t)
	first, err := Listen(name)
	if err != nil {
		t.Fatalf("could not create the first instance: %v", err)
	}
	defer first.Close()

	second, err := Listen(name)
	if err == nil {
		second.Close()
		t.Fatal("a second listener took the same pipe name. FILE_FLAG_FIRST_PIPE_INSTANCE " +
			"is what stops a second process attaching an instance to a pipe it does not own.")
	}
}

// openPipeForRead opens an existing pipe with READ_CONTROL, which is the least
// that lets its descriptor be read.
func openPipeForRead(name string) (windows.Handle, error) {
	namep, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(namep, windows.READ_CONTROL, 0, nil, windows.OPEN_EXISTING, 0, 0)
}
