# packaging

Everything a release needs that is not the binary itself. The decisions behind
these files — target list, platform labels, why the scoop bucket is in this
repo, and what is deliberately not published — are recorded once in
[docs/design.md §8](../docs/design.md); this file is the runbook, not the
argument.

## Tag day is one command

```
git tag v0.2.0 && git push origin v0.2.0
```

**v0.2.0, not v0.1.0.** A `v0.1.0` ref already exists on `origin`, at
`dd9d1d7`. That commit predates `release.yml`, so the tag fired no run and no
release exists. The owner ruled on 2026-08-11 that this ref stays where it is.
The first release therefore cuts at a higher version.

The changelog carries a known consequence, and the owner ruled it intended on
2026-08-12: goreleaser diffs from the previous tag, so the v0.2.0 release
notes start at PR #130 and do not restate the history before `v0.1.0`.

That is the whole of it. `.github/workflows/release.yml` fires on the `v*` tag
and does the rest:

1. runs **the repo's own gate** — `.github/workflows/ci.yml`, called rather
   than copied: vet, the suite, the build, and the binary-level smokes on
   `windows-latest`; the race detector on `ubuntu-latest`; and the suite plus
   the smokes again on `macos-latest`, which is Apple Silicon. A red gate on
   any of the three stops the release here.
2. runs goreleaser, which cross-compiles the four targets with CGO off, stamps
   `main.version` with the tag, archives them (zip on Windows, tar.gz
   elsewhere) with `checksums.txt`, and creates a **draft** GitHub release
   carrying the platform-label table.
3. commits the scoop manifest to `bucket/telltale.json` and the Homebrew
   formula to `Formula/telltale.rb` on `main`, as two commits authored
   `goreleaser`.

Then, by hand:

4. **Publish the draft.** Review the notes, press publish. This is the step
   that is deliberately not automated — publishing is outward-facing, and it
   is also what makes the download URLs in the scoop manifest and the formula
   resolve. Step 3 lands both a few minutes ahead of that, so publish
   promptly: in the window between, `scoop install telltale` and
   `brew install telltale` would 404. Each fails cleanly and installs nothing,
   but it is a real window and the fix is to not leave the draft sitting.
5. **Submit to winget** (below), if you want that channel for this release.

Verify the result the way a user would, on each machine:

```
scoop bucket add telltale https://github.com/sanlee-ys/telltale
scoop install telltale
telltale version
telltale council
```

```
brew tap sanlee-ys/telltale https://github.com/sanlee-ys/telltale
brew install telltale
brew test telltale
telltale version
telltale council
```

`brew test` runs the formula's own check: `telltale version` must print the
formula's version, which a source build (`dev`) never does.

## Verifying the release config without tagging

`--snapshot` builds everything and publishes nothing. `skip_upload: auto` in
`.goreleaser.yaml` means the scoop and Homebrew publishers both sit out a
snapshot, so this cannot touch `main`:

```
goreleaser check
goreleaser release --snapshot --clean
./dist/telltale_windows_amd64_v1/telltale.exe version
cat dist/homebrew/Formula/telltale.rb
```

**`goreleaser check` exits 2 since the tap landed, and the reason is one
line.** goreleaser v2.17.1 (the version `release.yml` pins) deprecates
`brews` in favour of `homebrew_casks`, and `check` fails on any deprecated
key. Measured 2026-09-02: the output is exactly one `DEPRECATED: brews`
notice followed by `configuration is valid, but uses deprecated properties`,
and `goreleaser release --snapshot --clean` prints the same notice and
finishes at exit 0 with the formula written under `dist/homebrew/`. Read
`check` as green when that notice is the only thing it reports; anything else
is a real problem. The formula stays a formula on purpose, and
`.goreleaser.yaml`'s comment above the `brews` block says why a cask would
make a claim SECURITY.md does not.

`dist/` is gitignored; delete it when you are done. goreleaser installs
user-local with `go install github.com/goreleaser/goreleaser/v2@latest`, which
lands the binary in `$(go env GOPATH)/bin` and needs no admin rights and no
package manager.

## install.ps1 — the one-paste Windows route

```
irm https://raw.githubusercontent.com/sanlee-ys/telltale/main/packaging/install.ps1 | iex
```

The script reads the latest release, downloads
`telltale_<version>_windows_amd64.zip` and `checksums.txt`, compares the
SHA-256 **before** it unpacks anything, copies `telltale.exe` into
`%LOCALAPPDATA%\Programs\telltale`, and adds that directory to the user `PATH`.
No administrator rights, no machine-wide setting. It then prints that the
binary is unsigned, and names `telltale doctor` as the next command.

The directory is **appended** to `PATH`, never prepended. A `telltale.exe`
already earlier on `PATH` goes on winning, and `Get-Command telltale` names the
one that runs. A script that silently outranks a binary the operator put there
is doing something the operator did not ask for.

Three environment variables, because a piped script takes no parameters:

| Variable | Effect |
|---|---|
| `TELLTALE_VERSION` | Install this tag instead of the latest release. |
| `TELLTALE_INSTALL_DIR` | Put `telltale.exe` here instead of the default. |
| `TELLTALE_NO_PATH=1` | Skip the user `PATH` edit. |

Exercise it without touching your own machine:

```powershell
$env:TELLTALE_INSTALL_DIR = "$env:TEMP\telltale-trial"
$env:TELLTALE_NO_PATH = '1'
irm https://raw.githubusercontent.com/sanlee-ys/telltale/main/packaging/install.ps1 | iex
& "$env:TEMP\telltale-trial\telltale.exe" doctor
```

Two rules for anyone editing this file.

**Keep every character under 0x80.** Windows PowerShell 5.1 reads a BOM-less
file as ANSI, so one em dash in a string breaks the parse before the script
runs. Measured 2026-08-18: an em dash in one `throw` produced four parser
errors under 5.1.26100.9168 and none under PowerShell 7.6.5. The `irm | iex`
path decodes UTF-8 and hides this; the download-then-run path does not.

**Never call `exit`.** A piped script runs inside the user's shell, so `exit`
ends their session. The body is a function, every failure is a `throw`, and one
`try/catch` at the bottom prints the reason.

What the script deliberately does not do: sign anything, prepare a signing
pipeline, or install on `windows/arm64`. The first two are the owner's decision
([docs/design.md §8](../docs/design.md), item 8). The third has no binary to
install, so the script refuses that machine by name.

## The Homebrew tap

`Formula/telltale.rb` is the tap, and this repository is its remote. A user
runs:

```
brew tap sanlee-ys/telltale https://github.com/sanlee-ys/telltale
brew install telltale
```

`brew tap <user>/<name> <url>` accepts any git repository with a `Formula/`
directory, so the tap needs no `homebrew-telltale` repository; the URL is what
makes the short name resolve. The formula picks `darwin_arm64` on Apple
Silicon, `darwin_amd64` on Intel, and `linux_amd64` on Linux, verifies each
against the sha256 goreleaser wrote into it, and installs the one binary. It
is a formula rather than a cask because Homebrew fetches a formula's archive
with curl and writes no `com.apple.quarantine`, so the unsigned binary runs
as installed; a cask arrives quarantined and needs an `xattr` hook to run at
all (`.goreleaser.yaml`, the comment above `brews`).

**The next release is what makes the tap move.** The runbook is one
paragraph. Tag (`git tag vX.Y.Z && git push origin vX.Y.Z`). `release.yml`
fires, runs `ci.yml` as its gate (Windows, the race job, and the darwin job
on Apple Silicon), then runs goreleaser, which builds the archives, stages the
draft release, and commits `Formula/telltale.rb` to `main` with the new
version, URLs and sha256 values using the workflow's own `GITHUB_TOKEN`, the
same way it commits `bucket/telltale.json`. Publish the draft promptly; until
it is published the URLs in the fresh formula 404. Then, on a Mac,
`brew update && brew upgrade telltale && brew test telltale`. Nothing about
the tap is done by hand at a release, and nothing about it needs a secret
beyond the token the release already holds.

**The checked-in formula predates the first automated one.** It names
`v0.2.0`, with the four sha256 values taken from that release's
`checksums.txt` on 2026-09-02, in the shape goreleaser v2.17.1 writes
(verified against a `--snapshot` run's output). goreleaser overwrites it
whole at the next tag. Nobody has run `brew install telltale` against it yet;
the first install is owed and belongs in PARITY.md when it happens.

**An `-rc` tag leaves the formula alone.** `skip_upload: auto` skips the
publisher on a prerelease, the same rule the scoop bucket follows, so a
rehearsal tag never moves the formula users installed from.

## winget

The three manifests in `winget/` are a **draft**. They are not submitted, and
`telltale` is not in `microsoft/winget-pkgs` — the name was checked free there
(and in the scoop `Main`/`Extras` buckets) at packaging time.

Submission is an external pull request to a Microsoft-owned repository, so it
stays a human action. The flow, on the day:

1. Publish the GitHub release first. winget validation downloads the installer
   URL, so it must resolve.
2. Get the archive's hash. `checksums.txt` on the release already has it, or:

   ```
   winget hash https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_windows_amd64.zip
   ```

3. Copy `winget/` to `manifests/s/sanlee-ys/telltale/<version>/` in a fork of
   `microsoft/winget-pkgs`, and replace the three placeholders in all three
   files: `{{VERSION}}` (the version WITHOUT the leading `v` — `0.2.0`, not
   `v0.2.0`; the tag keeps its `v` only inside the URL), `{{SHA256}}`, and
   `{{RELEASE_DATE}}` (`YYYY-MM-DD`).
4. Validate locally before opening the PR:

   ```
   winget validate --manifest manifests/s/sanlee-ys/telltale/0.2.0
   winget install --manifest manifests/s/sanlee-ys/telltale/0.2.0
   telltale council
   ```

5. Open the PR against `microsoft/winget-pkgs`. Their bot runs the same
   validation plus an install test in a sandbox.

Only `windows_amd64` is listed, because winget installs on Windows and
`windows/arm64` is not built at all. The package shape is
`InstallerType: zip` + `NestedInstallerType: portable` — telltale is one exe
in a zip with no installer, the same shape fzf and zoxide use.

**Do not automate this into the release workflow.** A bot that opens PRs
against someone else's repository on every tag is a different thing from a
release, and the version cadence here does not need it.

## What is not packaged, and why

`.goreleaser.yaml` carries the full list in a comment at the bottom; the short
form is that each omission is the packaging version of the honest-gauge rule.
The Homebrew tap above is packaged as of 2026-09-02; the entry that used to
refuse it (macOS smoke-verified on Intel only, Apple Silicon `built, not
verified`) was retired by `ci.yml`'s darwin job running the binary on Apple
Silicon on every commit, which is the measurement that entry was waiting for.
What is still not done, each with its reason:

- **homebrew-core.** Not submitted. Its acceptance rules ask for a notable
  project, with 75 GitHub stars among the signals it lists, and nothing here
  measures that number (README's badge rule says why nothing renders it
  either). The tap is the channel until then, and the formula is already in
  the shape a core submission starts from.
- **winget.** Drafted, not submitted (above). Submission is a pull request to
  a Microsoft-owned repository and stays a human action.
- **Signing and notarization.** Not built, and the decision is the owner's
  (design.md §8, item 8): both need a credential the owner holds, Apple's
  costs a yearly fee, and no arrangement of the workflow produces a signature
  without one. The tap does not change this. It changes how the binary
  arrives, and SECURITY.md says what that does and does not buy.
- **A Homebrew cask.** A cask arrives quarantined, and the only way to make an
  unsigned cask run is a post-install `xattr` hook that goreleaser's own
  documentation labels a Gatekeeper bypass. The formula never sets the mark.
- **`.deb` or `.rpm`.** A distro package is a support claim Linux has not
  earned here; `linux_amd64` is still `built, not run`.
- **npm.** The bare name is taken by an unrelated package, and design.md §6.5
  already recorded npm as optional for a Go binary, so it is skipped, not
  renamed.
