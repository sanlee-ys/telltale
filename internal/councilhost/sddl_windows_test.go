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

// everyoneSID and everyoneAlias are the two spellings of the group the DEFAULT
// named-pipe descriptor admits, and the one this design exists to keep off the
// pipe.
//
// BOTH spellings are checked, and that is not belt-and-braces: the first
// version of this test looked only for the raw SID and the measurement came
// back with the alias, so the test would have reported the pipe clean while
// Everyone was on it. Windows renders a well-known SID as its two-letter alias
// in an SDDL string, and which one you get is the API's choice rather than the
// object's state.
const (
	everyoneSID   = "S-1-1-0"
	everyoneAlias = ";WD)"
)

// anonymousSID and anonymousAlias are ANONYMOUS LOGON, the second account the
// default admits, in the same two spellings.
const (
	anonymousSID   = "S-1-5-7"
	anonymousAlias = ";AN)"
)

// admitsEveryone reports whether a DACL string carries an entry for Everyone or
// for the anonymous account, in either spelling.
func admitsEveryone(dacl string) bool {
	return strings.Contains(dacl, everyoneSID) || strings.Contains(dacl, everyoneAlias)
}

func admitsAnonymous(dacl string) bool {
	return strings.Contains(dacl, anonymousSID) || strings.Contains(dacl, anonymousAlias)
}

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
// documentation page (ADR-001, design.md §4a.1). So this creates exactly such a
// pipe and reads its DACL back.
//
// It is the NEGATIVE CONTROL for the test below, and without it that test is
// much weaker than it looks: on its own,
// TestThePipeCarriesTheExplicitDescriptor proves only that a string was
// applied. This is what proves the string PREVENTS something — that the default
// really would have put the operator's conversation in front of every local
// account.
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

	dacl, err := readPipeDACL(h)
	if err != nil {
		t.Fatalf("could not read the default pipe's DACL: %v", err)
	}
	t.Logf("measured default named-pipe DACL on this machine: %s", dacl)
	if !admitsEveryone(dacl) {
		t.Skipf("this Windows build's default named-pipe DACL admits neither %s nor %s "+
			"(%s) — design.md §7.28's claim about the default was MEASURED on a build "+
			"that did, and a build that tightened it should be re-measured rather than "+
			"assumed", everyoneSID, everyoneAlias, dacl)
	}
	if !admitsAnonymous(dacl) {
		t.Logf("this build's default admits Everyone but NOT the anonymous account (%s). "+
			"§7.28 names both; the Everyone half is what the refusal rests on.", dacl)
	}
}

// TestThePipeCarriesTheExplicitDescriptor is the positive half.
//
// It asserts what the OBJECT carries, not what was requested. Passing a
// SecurityAttributes proves an intention; reading the DACL back off the live
// handle with GetSecurityInfo proves the pipe. The difference matters because a
// descriptor that failed to apply would leave the default in place, which is
// precisely the leak measured above.
//
// Three assertions, and each one is a separate thing that could go wrong:
//
//  1. The three intended SIDs are present, so the pipe is usable by the host,
//     by an administrator debugging it, and by LocalSystem.
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

	// The listener's handle is unexported on purpose, so the DACL is read the
	// way any other process would read it: by name, through GetNamedSecurityInfo.
	// That is a stronger claim than reading the creator's own handle — it is
	// what the object presents to the world.
	dacl, err := readNamedPipeDACL(name)
	if err != nil {
		t.Fatalf("could not read the pipe's DACL by name: %v", err)
	}
	t.Logf("applied SDDL: %s", ln.SDDL())
	t.Logf("pipe DACL as the object carries it: %s", dacl)

	// Matched on the ACE's trailing SID field (";SY)") rather than on the bare
	// token, so that a two-letter alias cannot be found by accident inside an
	// unrelated part of the string.
	for _, want := range []string{"SY", "BA", mine.String()} {
		if !strings.Contains(dacl, ";"+want+")") && !strings.Contains(dacl, ";"+sidAlias(want)+")") {
			t.Errorf("the pipe's DACL does not admit %s — a descriptor that failed to "+
				"apply leaves the DEFAULT in place, which is the leak this test's "+
				"negative control measures.\n  DACL: %s", want, dacl)
		}
	}
	if admitsEveryone(dacl) {
		t.Fatalf("the pipe admits Everyone. This pipe carries agent transcript content "+
			"and a dispatch channel, so that is every local account able to read the "+
			"operator's conversation — the exact shape of mistake design.md §7.24 was "+
			"written about.\n  DACL: %s", dacl)
	}
	if admitsAnonymous(dacl) {
		t.Fatalf("the pipe admits ANONYMOUS LOGON.\n  DACL: %s", dacl)
	}
	if !strings.HasPrefix(ln.SDDL(), "D:P") {
		t.Errorf("the descriptor is not a PROTECTED DACL, so an inherited entry could "+
			"widen it: %s", ln.SDDL())
	}
}

// TestTheDescriptorNamesThisUserLiterally pins the OW ruling.
//
// design.md §7.28 rules the literal SID over CREATOR OWNER, because OW is a
// placeholder an object substitutes at creation and whether a named pipe
// substitutes it the way a file does was never measured. A later session
// tidying the string to OW would be re-opening a question this design declined
// to depend on, so the refusal is pinned rather than left in prose.
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

// sidAlias maps a two-letter SDDL alias to the SID it stands for, so the DACL
// check passes whether Windows renders the entry as an alias or as a SID.
func sidAlias(s string) string {
	switch s {
	case "SY":
		return "S-1-5-18"
	case "BA":
		return "S-1-5-32-544"
	default:
		return s
	}
}

// readNamedPipeDACL reads a pipe's DACL by NAME rather than through the
// creator's handle.
func readNamedPipeDACL(name string) (string, error) {
	namep, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(namep, windows.READ_CONTROL, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return readPipeDACL(h)
}
