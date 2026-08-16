<#
.SYNOPSIS
  A one-line fleet segment for a PowerShell prompt, from one `telltale snapshot` call.

.DESCRIPTION
  The worked example of consuming `telltale snapshot --compact` from a program:
  one process, one JSON parse, one line of text. `docs/snapshot.schema.json` is
  the contract it reads against, and `docs/design.md` §7.22 is the record.

  It is deliberately small, because the point is the contract and not the
  renderer. What it shows is how a consumer keeps the document's two honesty
  rules on the way to a string:

  - A NULL PRINTS NOTHING. `context_pct_max` and a quota window's `used_pct`
    are nullable. When there is no reading, the whole segment is missing from
    the line. It never falls back to 0, and it never prints a dash that a
    reader could mistake for a measurement. This is the zero-vs-absent rule
    (design.md §4a.1) carried into a caller: `if ($null -eq $v)` and never
    `if (-not $v)`, because `-not 0` is true and a measured zero would vanish.
  - A DERIVED NUMBER KEEPS ITS MARK. When the vendor holding the highest
    context percentage lists `context_pct` in `estimated`, the figure prints
    with the leading `~` the gauges use. The document says which numbers were
    computed rather than read, so a consumer that drops that says more than it
    measured.

  Two smaller decisions, said out loud:

  - It refuses a document whose `schema_version` is not 1 and prints nothing.
    A prompt segment that guesses at an unknown contract is worse than a prompt
    segment that is absent for one release.
  - `scan_error` prints as the words `scan degraded` and never as its own text.
    The message can be long enough to break a prompt, and it can name a path.

  Output is ASCII and carries no colour or ANSI escapes. The colour is the
  caller's to add, and the line has to survive a console that has neither.

  Windows PowerShell 5.1 is the target, because that is what a Windows 11 box
  has before anyone installs anything, and this project's primary platform is
  Windows (ADR-002). Nothing here needs PowerShell 7: no ternary operator, no
  null-coalescing, no `ConvertFrom-Json -Depth` and no `-AsHashtable`. It runs
  unchanged on PowerShell 7.

.PARAMETER Telltale
  The binary to run. Defaults to `telltale` on PATH.

.PARAMETER FromFile
  Read a document from a file instead of running the binary. This is how the
  degraded shapes were driven on a healthy machine: the four documents in
  `internal/snapshot/testdata/golden/` carry a refused store, a drifted store,
  a scan error and the zero-vs-absent pair, which a working fleet cannot
  produce on demand.

.EXAMPLE
  . .\tools\fleet-prompt.ps1
  function prompt { "$(Get-TelltaleFleetLine)`nPS $($ExecutionContext.SessionState.Path.CurrentLocation)> " }

.EXAMPLE
  Get-TelltaleFleetLine -FromFile internal\snapshot\testdata\golden\watching.json
#>

function Get-TelltaleFleetLine {
    [CmdletBinding()]
    param(
        [string] $Telltale = 'telltale',
        [string] $FromFile
    )

    if ($FromFile) {
        $raw = Get-Content -Path $FromFile -Raw
    } else {
        $raw = (& $Telltale snapshot --compact 2>$null) -join ''
        if ($LASTEXITCODE -ne 0) { return '' }
    }

    try { $doc = $raw | ConvertFrom-Json } catch { return '' }
    if ($doc.schema_version -ne 1) { return '' }

    $fleet = $doc.fleet
    $parts = @('tt {0} watching' -f $fleet.vendors_watching)
    $parts += '{0} sessions, {1} live' -f $fleet.sessions, $fleet.live

    $off = @($doc.vendors | Where-Object { $_.status -ne 'watching' })
    if ($off.Count -gt 0) {
        $named = ($off | ForEach-Object { '{0} {1}' -f $_.vendor, $_.status }) -join ', '
        $parts += 'attn {0}: {1}' -f $off.Count, $named
    }

    # A null context reading drops the segment. A measured 0 prints as 0%.
    if ($null -ne $fleet.context_pct_max) {
        $top = $doc.vendors |
            Where-Object { $_.context_pct_max -eq $fleet.context_pct_max } |
            Select-Object -First 1
        $mark = ''
        if ($top -and ($top.estimated -contains 'context_pct')) { $mark = '~' }
        $seg = 'ctx {0}{1:0.#}%' -f $mark, $fleet.context_pct_max
        if ($top) { $seg = '{0} {1}' -f $seg, $top.vendor }
        $parts += $seg
    }

    # The busiest relayed window. A window with no figure yet is skipped, not
    # counted as 0 -- `quota: []` and `used_pct: null` are both "no reading".
    $worst = $null
    foreach ($vendor in $doc.vendors) {
        foreach ($window in $vendor.quota) {
            if ($null -eq $window.used_pct) { continue }
            if ($null -eq $worst -or $window.used_pct -gt $worst.pct) {
                $worst = [pscustomobject]@{
                    pct    = $window.used_pct
                    vendor = $vendor.vendor
                    label  = $window.label
                }
            }
        }
    }
    if ($null -ne $worst) {
        $parts += 'quota {0:0.#}% {1}/{2}' -f $worst.pct, $worst.vendor, $worst.label
    }

    if ($null -ne $doc.scan_error) { $parts += 'scan degraded' }

    return ($parts -join ' | ')
}
