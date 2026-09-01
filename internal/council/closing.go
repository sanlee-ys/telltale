package council

// The closing line: what quitting the room actually did (design.md §9.52,
// rung 0).
//
// Quitting has always killed every seat. `teardown`'s own doc comment states
// why — an agent that outlives the window showing it is the invisible state
// this product refuses — and nothing about that behaviour changes here. What
// changes is that the room says it.
//
// It was worth saying because the operator had no way to learn it except by
// noticing, some hours later, that a conversation had gone cold. A contract
// nobody can read is not a contract the operator can plan against, and this one
// decides whether closing a terminal window is cheap or expensive.
//
// STDOUT, AND AFTER THE ALTERNATE SCREEN IS RELEASED. There is no frame left to
// draw a card in by the time this is true, which is the same reason a failed
// save is printed at that point rather than raised as a notice. A notice set
// during teardown lands on a Model nobody will ever view again.
//
// A PURE FUNCTION OVER FOUR MEASURED VALUES, so the wording is testable without
// a terminal, a process or a Model. Nothing here reads the clock, the
// filesystem or the environment: the caller in Run has all four facts already.

// closingLines is what the room prints on the way out.
//
// ended is how many vendor processes teardown killed, turn is the last turn the
// saved room records, roomPath is where that room lives (empty when the path
// could not be resolved at all) and home abbreviates it for display.
//
// THE ZERO IS A SENTENCE, NOT A NUMBER. A room that spawned nothing says "no
// vendor process was running" rather than "0 vendor processes ended". Both are
// true and only one answers the question the reader has, which is whether
// anything was left behind. This is §4a.1's rule applied to prose: a measured
// zero and an absence must not be read as each other, and here the honest
// spelling of the zero is a different sentence rather than a different glyph.
//
// NO MARK, NO COLOUR, NO GLYPH. Ending the seats is the room working, and a
// warning spent on correct behaviour is a warning the eye stops reading
// (§9.19's argument, one surface out). Plain lines also survive a pipe, which
// is where this text lands when someone captures a session.
func closingLines(ended, turn int, roomPath, home string) []string {
	out := []string{"telltale council: " + endedClause(ended)}

	switch {
	case turn <= 0:
		// No completed turn means no saved room: SaveRoom is only reached with
		// a turn behind it, and readRoom refuses a turn:0 file outright. Saying
		// "the ids are saved" here would point the reader at a file that does
		// not exist.
		out = append(out, "no turn completed, so nothing was saved — the next telltale council opens a fresh room.")
	case roomPath == "":
		// The state directory could not be located at all (no resolvable home).
		// The turn number is still a fact this room measured, so it is still
		// reported; where it went is not, so that half is not claimed.
		out = append(out, "turn "+itoa(turn)+" was the last. the saved room's location could not be resolved, so what is on disk is unknown.")
	default:
		// The split that matters, and the reason this line exists at all: the
		// ids survive and the conversation does not. Saying only the first half
		// would let room.json be read as a transcript, which resume.go is
		// emphatic it has never been.
		out = append(out, "the conversation is gone. the session ids are not — "+
			abbreviate(roomPath, home)+" holds them, at turn "+itoa(turn)+".")
		// Named in §9.52's vocabulary rather than as "reattaches", because what
		// the next launch does to a SEAT is start a new process. `reattach` is
		// the file half and it is already taken; using it here would make the
		// one sentence about processes say the word that means the file.
		out = append(out, "telltale council rebuilds those seats: a new process on each saved id, "+
			"which spends the startup at room open instead of on your first brief.")
	}
	return out
}

// endedClause is the first sentence: what happened to the vendor processes.
//
// The trailing half is the contract itself, stated once. A seat never outlives
// the room, on purpose, and the operator who reads this line has just watched it
// happen — which is the one moment the rule costs nothing to learn.
func endedClause(ended int) string {
	if ended <= 0 {
		return "the room is closed. no vendor process was running, so none was ended."
	}
	// NOT plural(): that helper appends a bare "s", which is right for every
	// word it already serves ("path", "commit", "seat") and wrong for this one.
	// Spelled here rather than by teaching the shared helper a sibilant rule,
	// because one caller is not enough evidence to change a function five
	// others depend on.
	word := "processes"
	if ended == 1 {
		word = "process"
	}
	return "the room is closed. " + itoa(ended) + " vendor " + word +
		" ended with it — a seat never outlives the room."
}
