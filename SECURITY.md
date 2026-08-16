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

**Three bounded write exceptions exist.** All three write under `~/.telltale/`,
and all three write numbers and keys only, never session content. `telltale
council` writes `council/room.json` (session ids and the workspace path). The
statusline writes `quota/<vendor>.json` (the rate-limit windows it just
rendered). The relays write `usage/<vendor>.json` (per-turn token totals).
`telltale hook cursor` and `telltale otel grok` are the two writers of that last
file. The OTLP listener binds the loopback interface only, and the vendor pushes
to it, so the gauges still make no network calls of their own. A test pins the
serialized form of each of the three files to keys and numbers.

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

**No release artifact is signed.** The release workflow builds and stages the
artifacts, and it runs no signing step. You can check this: `.goreleaser.yaml`
declares no `signs` block, and `.github/workflows/release.yml` has no signing
step and holds no signing secret.

The consequences, per platform:

- **Windows.** The binary carries no Authenticode signature. `scoop` and `winget`
  install that same unsigned binary. A direct download through a browser can
  raise a Microsoft Defender SmartScreen prompt, because SmartScreen weighs the
  signature and the download reputation. This project has not measured that
  prompt.
- **macOS.** The `darwin_amd64` and `darwin_arm64` archives are unsigned and not
  notarized. macOS applies the `com.apple.quarantine` attribute to a file that a
  browser downloads, and Gatekeeper then refuses to run an unsigned, un-notarized
  binary that carries that attribute. This project has not run that path: the
  macOS smoke test ran a binary built on the Mac itself, not a downloaded release
  archive.
- **Linux.** The archive is unsigned. Linux applies no equivalent gate, so the
  archive runs after you unpack it.

**Verify the checksum instead.** Every release attaches `checksums.txt` with a
SHA-256 for each archive. That file tells you the archive is the one the release
workflow produced. It does not tell you who produced it, which is the property a
signature adds and this project does not yet provide. `scoop` verifies the
SHA-256 itself from the manifest.

Signing is not planned work with a date. It needs a certificate or an Apple
Developer account that the owner holds, plus release secrets, so it is an owner
decision rather than a contributor task. `docs/design.md` §8 records it as one.
