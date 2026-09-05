# The demo-tape recording chain

The fallback tape is a recording of the demo path. `STATE.md` holds the five
beats and the rule that binds them: capture the demo, do not invent it. This
file records the chain that captures it, and every claim here comes from a run
on the reference box.

**This directory contains no tape and no cast.** That is a rule, not an
omission. See "Casts stay out of this repository" below.

## The chain

Two tools. PowerSession-rs records a real Windows console session to an
asciicast file. agg renders that file to an animated GIF.

```
telltale surface  ->  PowerSession rec  ->  *.cast  ->  agg  ->  *.gif
```

**VHS is not the chain.** VHS cannot record on this machine
(charmbracelet/vhs #631, dead since 2025-06). VHS also renders xterm.js. This
project's reference environment is Windows Terminal (ADR-002), so a VHS tape
would show a terminal the product does not target.

## Install

Both tools are on winget. Both install to the user scope. Neither needs
administrator rights. Measured 2026-08-16:

```powershell
winget install --id Watfaq.PowerSession --scope user
winget install --id asciinema.agg --scope user
```

Then check the tools, and record one surface to prove the chain still works:

```powershell
PowerSession --version
agg --version
.\packaging\tape\record.ps1 -Command "telltale hud" -Cast $env:TEMP\hud.cast `
    -Gif $env:TEMP\hud.gif -Select "60%" -Unattended -DwellSeconds 8 -Keys q
```

Both packages are portable installs. winget puts a shim in
`%LOCALAPPDATA%\Microsoft\WinGet\Links` and adds that directory to the user
PATH. A shell that started before the install keeps the old PATH, so start a
new shell or call the shim by its full path. `record.ps1` resolves the shim
itself for this reason.

Versions and maintenance state at install time:

| Tool | winget id | Version | Released | Licence |
| --- | --- | --- | --- | --- |
| PowerSession-rs | `Watfaq.PowerSession` | 0.1.16 | 2026-06-11 | MIT |
| agg | `asciinema.agg` | 1.9.0 | 2026-05-29 | GPL-3.0 |

Both projects are active. PowerSession-rs 0.1.16 took dependency updates and
two outside contributions. agg 1.9.0 added glyph and frame-selection features.
Both declare `Microsoft.VCRedist.2015+.x64` as a dependency, and winget
installs it if it is absent.

To remove both tools:

```powershell
winget uninstall --id Watfaq.PowerSession
winget uninstall --id asciinema.agg
```

## What was proven

Three captures against the real binary, in ascending difficulty.

### 1. Plain output records correctly

`telltale doctor` recorded to an asciicast v2 file: 120x30, 7 events, 5659
bytes. agg rendered it. The text is legible at the default font size.

This capture proves plain output only. It does **not** prove colour.
`telltale doctor` emits no colour by design (`internal/doctor/view.go`), so the
cast correctly contains one SGR sequence and no colour codes.

### 2. The alternate-screen TUI records correctly

`telltale hud` recorded to an asciicast v2 file: 120x30, 31 events, 11905
bytes. Three properties were measured in the cast bytes:

- **Alternate screen.** `ESC[?1049h` appears once and `ESC[?1049l` appears
  once. The recorder captures both the entry and the exit.
- **Colour.** The cast carries 47 cyan, 19 green and 7 bright-black
  foreground codes, plus 104 faint and unfaint pairs. These are ANSI palette
  indices, which is what telltale emits by design (CLAUDE.md: the terminal
  resolves the palette against its own theme).
- **Restore.** See test 3.

The rendered GIF is legible and keeps the distinctions this product exists to
make. The `~` estimate marker and the em-dash absent mark both survive.

### 3. The restore returns the real screen

A test recorded three steps in one session: a marker line, then
`telltale hud`, then a second marker line. The final rendered frame shows both
marker lines and no HUD content. The primary screen buffer came back intact.
agg's terminal emulator handles the alternate-screen save and restore
correctly.

This test matters more than the escape-code count. A recorder can capture
`ESC[?1049l` and still produce a GIF with TUI residue on the final frame.

## The honest limits

Each limit below was measured, not assumed.

**A recording needs a real console.** PowerSession drives ConPTY, so it needs a
console for its own standard input and output. Run from an agent harness, whose
standard input is the null device, it panicked four times and wrote a 149-byte
cast with no session content in it. The panic text names `pty stdin closed`.
The owner records from Windows Terminal, so this limit costs the demo tape
nothing. It does block an unattended capture, which is why `record.ps1` gives
the recorder a hidden console in `-Unattended` mode.

**Check `NO_COLOR` before you record.** The first HUD capture came back with no
colour at all. The cause was `NO_COLOR=1` in the recording shell's environment,
not the recorder. A tape can lose every hue this way and still look like a
successful run. Read the environment first:

```powershell
Get-ChildItem Env:NO_COLOR, Env:WT_SESSION, Env:TERM, Env:COLORTERM -ErrorAction SilentlyContinue
```

**A TUI's geometry is fixed when you record it.** agg's `--rows` and `--cols`
re-run the recorded byte stream through a terminal of the size you ask for.
That recovers output which scrolled away, but only for a program that does not
read the terminal size. Both sides were measured:

- `telltale doctor` writes 76 lines and does not read the terminal size. A cast
  recorded at 30 rows and rendered with `--rows 82` shows almost the whole
  report.
- `telltale hud` draws to the size it read. The same cast rendered with
  `--rows 50` puts 20 empty rows below the footer. The layout does not grow.

So size the terminal window before you record the tape. For the HUD and the
council room, the recorded size is the only size.

**agg resolves the palette against its own theme.** telltale emits ANSI palette
indices, so the GIF's hues come from agg's theme and not from Windows
Terminal's. `agg --theme` selects from twelve built-in themes plus `custom`.
Pick the theme at render time to match what the room looks like on the day.

**agg warns about the emoji font.** agg reports that `Segoe UI Emoji` carries
COLRv1 colour glyphs which its default `swash` renderer does not support, and
it falls back per glyph. Every glyph in the captures above rendered correctly,
including the box-drawing characters and the `⑂` sub-agent chip. agg names
`--renderer resvg` as the alternative. That renderer was **not** tested here.

**Watch the asciicast version.** PowerSession 0.1.16 wrote a v2 header in every
capture, and agg 1.9.0 read it. PowerSession 0.1.16 also added asciicast v3
support. If a later PowerSession writes v3 by default, check that agg reads it
before you trust a tape.

## Casts stay out of this repository

This repository is public. A cast of any real telltale surface carries content
that must not be published. Both captures were inspected and both fail the
test:

- the `telltale doctor` cast carries the absolute path of every vendor binary
  on the machine;
- the `telltale hud` cast carries live session names, workspace paths and
  session identifiers.

`record.ps1` also leaves PowerSession's `--stdin` flag off, which keeps the
operator's typing out of the file. That flag writes keystrokes into the cast as
`"i"` events, so it would capture the brief the owner types in beat 1. agg
renders only `"o"` events, so the flag costs content and buys the GIF nothing.
Keys still reach the recorded program without it, because the recorder always
forwards its own standard input down the pty. Both ways were measured
2026-08-16: with the flag the cast held 4 keystroke events, without it 0, and
the alternate screen and all 47 cyan codes were identical.

Redaction is not the answer, because the surfaces are the point of the tape.
Write casts and GIFs outside the working tree. `.gitignore` refuses
`*.cast` and `*.gif` as a backstop, but the backstop is not the rule. The rule
is that the tape is a personal artifact of one machine on one evening, and this
repository holds the script that makes it.

## What remains

The chain works. The tape does not exist yet, and no script can create it.

The owner must drive the five beats of the demo path in one recorded session.
`STATE.md` defines the beats and is the durable copy; the list below names only
the command each beat opens on, so this runbook says what it records without
holding a second copy of the path.

1. `telltale council`, then one brief to `@all`.
2. `ctrl+r`, the rebuttal.
3. A write brief, and `y` on the card the gated seat raises.
4. `/arena <brief>`, `/arena check <command>`, `/adopt`.
5. `q`, then `telltale council replay-check <file>`, then `telltale council
   --replay examples/demo.jsonl --replay-speed 8`.

Beat 5 records the room a second way. The session runs under `--record`, so the
tape and the recording capture the same run, and the recording plays back
without a cast. Keep the `--record` file outside the workspace: council warns
when it sits inside, and a seat can read a file it can reach.

Steps on the day:

1. Size the terminal window. The HUD and the room keep the size you record.
   Use the window that `STATE.md`'s demo geometry paragraph names.
2. Check `NO_COLOR` is not set.
3. Start the recording, then drive the five beats:

   ```powershell
   .\packaging\tape\record.ps1 -Command "pwsh -NoLogo" -Cast $HOME\Videos\telltale-demo.cast
   ```

4. Exit the shell to stop the recording.
5. Render the tape:

   ```powershell
   agg --theme asciinema $HOME\Videos\telltale-demo.cast $HOME\Videos\telltale-demo.gif
   ```

A staged tape is forbidden. The arena race in beat 4 must be a race that ran.
