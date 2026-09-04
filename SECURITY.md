# Security policy

## How to report a vulnerability

Report a vulnerability privately. Do not open a public issue for a defect that an
attacker can use.

Use GitHub private vulnerability reporting on this repository:
<https://github.com/sanlee-ys/telltale/security/advisories/new>. The report stays
private between you and the maintainer until an advisory is published.

If that page does not show a report form, open a normal issue that says only that
you have a security report. Do not put the defect in the issue. The maintainer
then opens a private advisory thread, and you send the details there.

**What to expect.** One person maintains this project. The response is best
effort. There is no service level agreement, no bounty, and no guaranteed
response time. You get an acknowledgement when the maintainer reads the report,
and a decision after that. A fix ships in a normal release.

**Which versions get fixes.** The most recent release gets fixes. Older tags do
not. Build from source, or install the most recent release, before you report a
defect.

## The trust model

Read this section first. It tells you which behavior is a defect and which
behavior is the design. `docs/design.md` records the same boundary in more
detail, and `CLAUDE.md` states it as a rule for contributors.

**The gauges read local files. They do not call the network.** `telltale
statusline`, `telltale hud` and `telltale snapshot` read the session files that
the vendor CLIs already write on this machine. They make no network calls. They
read no credentials. No keybinding changes vendor state.

**Four bounded write exceptions exist.** All four write under `~/.telltale/`,
and all four write numbers and keys only, never session content. `telltale
council` writes `council/room.json` (session ids and the workspace path). The
statusline writes `quota/<vendor>.json` (the rate-limit windows it just
rendered). The relays write `usage/<vendor>.json` (per-turn token totals).
`telltale hook cursor` and `telltale otel grok` are the two writers of that last
file. The OTLP listener binds the loopback interface only, and the vendor pushes
to it, so the gauges still make no network calls of their own. `telltale probe`
writes `probe/<vendor>.json`: the vendor id, the version string that binary
printed, the day, the telltale build that probed, and one result plus a
millisecond count for each of its three checks. A test pins the serialized form
of each of the four files to keys and numbers.

**The probe file is the strictest of the four, because its writer drives an
agent.** The brief, the reply, the session id the vendor named and the
directory the seat ran in never reach it. Neither does the failure reason: a
vendor's own first line of standard error routinely carries a path or a session
id, so `telltale probe` prints that line in the terminal where it ran and stops
there. `telltale doctor` reports which check failed and names the command that
shows why.

**The event sink is different, and it says so.** `telltale events` stores hook
payloads verbatim under `~/.telltale/events/`, so that directory holds content,
not just numbers and keys. Scope contains it rather than redaction: the operator
starts it as its own foreground mode, the server binds the loopback interface
only, and no gauge reads or renders those files.

**`telltale council` starts vendor CLIs.** It runs the vendor binaries that the
operator already installed and already trusts, in the operator's own workspace,
under the operator's own vendor credentials. telltale adds no account and holds
no token of its own. A seat can write to the workspace, and the room shows which
sandbox posture each seat runs under.

**`telltale probe` starts them too, and it spends a turn.** It brings each
installed seat up, sends a brief of one word, and times the stop. That is one
billed turn per seat, on the operator's own account and under the same
credentials. The mode says so before it starts, it asks at the terminal, and it
refuses to run when standard input is not a terminal unless `--yes` is given, so
a hook, a script or a CI step cannot reach it by accident. Every seat runs in a
throwaway empty directory that the mode makes and removes, never in the
operator's own workspace. Nothing else in the binary calls it: no gauge, no
room, and no scheduled path.

**Two adapters meet credential material, and both refuse it by construction.**
The Cursor adapter reads a store that holds OAuth and refresh tokens in the same
SQLite file as session state, so a read allowlist limits it, and a test plants
credential-shaped strings in fixtures and asserts that none reaches a surface the
HUD can display. The Cursor hook receives a payload that carries the model reply
text and the user email address, so the destination struct is the allowlist:
`encoding/json` drops every field with no destination, and a test asserts that no
planted marker survives the parser or the cache file.

**A report against any of the above is in scope.** Examples: credential-shaped
content that reaches a rendered surface or a cache file; a gauge that makes a
network call; a write outside `~/.telltale/`; session content in a file that the
rules above limit to numbers and keys; a council gate that approves a command it
should hold.

## Release artifact signing

**No release artifact carries a signature of its own.** No archive and no binary
holds an Authenticode signature, a codesign signature, or a notarization ticket.
The release workflow builds and stages the artifacts, and it adds none of these.
You can check this: `.goreleaser.yaml` declares no `signs` block, and
`.github/workflows/release.yml` holds no signing secret.

Read this together with the provenance section below. A release from the next
tag forward carries a signed provenance attestation, which is a separate signed
document *about* an archive. It is not a signature *on* an archive, and it does
not change any statement in this section.

The consequences, per platform:

- **Windows.** The binary carries no Authenticode signature. `scoop`, `winget`
  and `packaging/install.ps1` install that same unsigned binary, and the script
  says so in its own output before it names the next command. A direct download
  through a browser can raise a Microsoft Defender SmartScreen prompt, because
  SmartScreen weighs the signature and the download reputation. This project has
  not measured that prompt.
- **macOS.** The `darwin_amd64` and `darwin_arm64` archives are unsigned and not
  notarized. macOS applies the `com.apple.quarantine` attribute to a file that a
  browser downloads, and Gatekeeper then refuses to run an unsigned, un-notarized
  binary that carries that attribute. **This project has now walked that path, on
  2026-08-17**, on an Intel MBP on macOS 26.5.2, against the published
  `v0.2.0` `darwin_amd64` archive. macOS killed the binary. The terminal reported
  `Killed: 9` and exit status 137, the binary printed nothing, and a dialog read
  `"telltale" Not Opened` over `Apple could not verify "telltale" is free of
  malware that may harm your Mac or compromise your privacy.`, with the buttons
  `Move to Trash` and `Done`. `xattr -d com.apple.quarantine` cleared the
  attribute, and the binary then ran to completion at exit 0. **That walk used no
  browser.** `curl` fetched the archive, and the operator wrote the
  `com.apple.quarantine` attribute by hand to reproduce what a browser marks, so
  the gate and the remedy are measured and the download itself is reproduced. A
  real browser download is still owed, and `PARITY.md` records it as owed.

  **What CI measures on Apple Silicon (added 2026-09-02).** `ci.yml`'s `darwin`
  job builds telltale from the commit under test on `macos-latest`, which
  GitHub hosts on Apple Silicon, and runs it there: `go test ./...`,
  `telltale version`, `telltale doctor`, the statusline fixture smokes and
  `telltale council ls`, with the honesty assertions the Windows job makes.
  That runner has no vendor CLI, so it measures the honest states and never a
  live seat, and it runs a binary it built rather than an archive a release
  attaches. `darwin_arm64` is therefore "run", no longer "built, not run"; the
  archive itself has still not been unpacked and executed by hand on that
  platform.

  **The Homebrew tap sets no quarantine.** `brew install telltale` from
  `Formula/telltale.rb` fetches the release archive with `curl` into
  Homebrew's cache, and `curl` writes no `com.apple.quarantine` attribute
  (measured 2026-08-17 with `xattr -l`, above), so Gatekeeper is never
  consulted and the binary runs as installed. That is a property of the
  transport and not a signature: the archive the tap installs is the same
  unsigned, un-notarized one, and the formula is chosen over a cask precisely
  because a cask arrives quarantined and would need an `xattr` hook to run
  (`.goreleaser.yaml` records that choice). One difference between the two
  darwin archives comes from a source read rather than a run: Go's linker
  writes an ad-hoc code signature into darwin/arm64 output and into nothing
  else (`cmd/link/internal/ld/lib.go`, `NeedCodeSign`, Go 1.26.6), which is
  why the Intel walk saw `code object is not signed at all`. An ad-hoc
  signature names no developer and is not notarization. What Gatekeeper does
  with a quarantined `darwin_arm64` archive is unmeasured, because nobody has
  walked that archive by hand.
- **Linux.** The archive is unsigned. Linux applies no equivalent gate, so the
  archive runs after you unpack it.

**Verify the checksum.** Every release attaches `checksums.txt` with a SHA-256
for each archive. That file tells you the archive is the one the release workflow
produced. It does not tell you who produced it. `scoop` verifies the SHA-256
itself from the manifest, and `packaging/install.ps1` verifies it against
`checksums.txt` before it unpacks anything, deleting the download on a mismatch.

Signing is not planned work with a date. It needs a certificate or an Apple
Developer account that the owner holds, plus release secrets, so it is an owner
decision rather than a contributor task. `docs/design.md` §8 records it as one.

## Build provenance and the SBOM

**This section applies from the next tag forward.** The releases published today
carry neither of these. Nothing adds them to a release that already exists.

**Each archive gets a signed provenance attestation.** The release workflow uses
`actions/attest-build-provenance`. Verify one with the GitHub CLI:

```
gh attestation verify telltale_<version>_windows_amd64.zip --repo sanlee-ys/telltale
```

**What that proves, and what it does not.** A verified attestation proves that
the release workflow of this repository built that exact archive, from a named
commit, on a runner that GitHub hosts. It closes the gap that `checksums.txt`
leaves open, because a checksum proves only that two files match. It does not
prove the identity of the owner, and it does not prove that the owner vouches for
the content. A code-signing certificate proves that, and the section above says
why this project does not hold one.

The attestation needs no secret from the owner. GitHub mints a short-lived token
for each run, and that token is the identity. This is the reason provenance
exists here while signing does not: signing is blocked on a long-lived credential
and provenance needs none.

**Each archive also gets an SBOM.** syft writes one SPDX-JSON document per
archive, and the release attaches it. It lists the Go modules that the build
used. It does not describe the vendor CLI programs that `telltale council`
starts, because the operator installs those and this project ships none of them.

## Vulnerability scanning

Two scans run on this repository, and both also run on a weekly schedule. The
schedule is the part that matters: it catches a vulnerability that becomes public
after the code merged.

- **`govulncheck`** reads the Go vulnerability database and reports the known
  vulnerabilities that this code can actually reach. It fails on a finding.
- **CodeQL** runs GitHub's static analysis with the default query suite. Findings
  appear in this repository's Security tab.

A scan that fails does not mean a release is withheld. It means the maintainer
has to answer it. `docs/design.md` §8 records what each scan covers, and it
records the one file that each scan cannot see.
