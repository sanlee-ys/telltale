<#
.SYNOPSIS
  Records a telltale session to an asciicast, and optionally renders it to a GIF.

.DESCRIPTION
  The two halves of the recording chain, in one command. PowerSession-rs records
  a real Windows console session through ConPTY; agg renders the resulting
  asciicast to an animated GIF. README.md in this directory carries the
  measurements behind both, and the limits that are not obvious.

  Two modes, and the difference matters:

  - ATTENDED (the default, and the one the demo tape uses). The recorder runs
    in the terminal you are sitting in, and you drive the session by hand. The
    recorded geometry is your terminal's current size, so size the window
    BEFORE you start.
  - UNATTENDED (-Unattended). The recorder runs in a hidden console, the script
    waits -DwellSeconds, then types -Keys into it to quit the program. This
    mode is a smoke test of the chain, not a way to make a tape: nothing drives
    the session, so it captures a program starting and stopping and nothing in
    between.

  Never use this script to assemble a tape out of scripted keystrokes. The
  fallback tape captures the demo path a person drove; a staged one would be an
  invented recording of a race nobody ran.

.PARAMETER Command
  The command line to record. In attended mode this is usually a shell, so the
  operator can run the whole demo path inside one recording.

.PARAMETER Cast
  Output path for the asciicast. KEEP THESE OUT OF THIS REPOSITORY — a cast of
  any real telltale surface carries live session names and absolute user paths.
  README.md in this directory explains what was measured in one.

.PARAMETER Gif
  Optional. Render the cast to this GIF path after recording.

.PARAMETER Rows
  Optional agg row override. Safe only for output that does not query the
  terminal size; see README.md, which measured both sides of this.

.PARAMETER Cols
  Optional agg column override. Same caution as -Rows.

.PARAMETER Select
  Optional agg frame selector, e.g. '100%' for the final frame alone.

.PARAMETER Unattended
  Record in a hidden console and quit the program by typing -Keys into it.

.PARAMETER DwellSeconds
  Unattended only. How long to let the program render before typing -Keys.

.PARAMETER Keys
  Unattended only. The characters that quit the recorded program.

.EXAMPLE
  # The demo tape: a shell you drive by hand through the five beats.
  .\record.ps1 -Command "pwsh -NoLogo" -Cast $HOME\Videos\telltale-demo.cast

.EXAMPLE
  # Chain smoke test: does the HUD still record, colour and restore correctly?
  .\record.ps1 -Command "telltale hud" -Cast $env:TEMP\hud.cast `
               -Gif $env:TEMP\hud.gif -Unattended -DwellSeconds 8 -Keys q
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][string]$Command,
  [Parameter(Mandatory = $true)][string]$Cast,
  [string]$Gif,
  [int]$Rows,
  [int]$Cols,
  [string]$Select,
  [switch]$Unattended,
  [int]$DwellSeconds = 8,
  [string]$Keys = 'q'
)

$ErrorActionPreference = 'Stop'

# winget's portable installs land their shims in this Links directory and add
# it to the user PATH — but a shell started before the install has the old
# PATH, so resolve the shim directly rather than trusting Get-Command alone.
function Resolve-Tool {
  param([string]$Name)
  $onPath = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue
  if ($onPath) { return $onPath.Source }
  $shim = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Links\$Name.exe"
  if (Test-Path $shim) { return $shim }
  throw "$Name not found. Install it: winget install --id $(if ($Name -eq 'agg') { 'asciinema.agg' } else { 'Watfaq.PowerSession' }) --scope user"
}

$powerSession = Resolve-Tool 'PowerSession'
$castDir = Split-Path -Parent $Cast
if ($castDir -and -not (Test-Path $castDir)) { New-Item -ItemType Directory -Force $castDir | Out-Null }

# PowerSession takes the command as one quoted argument. Building the argument
# line by hand rather than handing Start-Process an array is deliberate: an
# array member containing spaces reached clap unquoted and exited 2.
#
# `--stdin` is deliberately absent. That flag records keystrokes into the cast
# as "i" events, and agg renders only "o" events — so it captures the
# operator's typing, including the brief, and buys the GIF nothing. Keys still
# reach the recorded program without it: the recorder always forwards its own
# standard input down the pty, and the flag only controls what it writes down.
# Measured 2026-08-16, both ways.
$argumentLine = 'rec -f -c "{0}" "{1}"' -f $Command, $Cast

if ($Unattended) {
  Write-Host "recording (unattended, hidden console): $Command"
  $recorder = Start-Process -FilePath $powerSession -ArgumentList $argumentLine `
                            -WindowStyle Hidden -PassThru
  Start-Sleep -Seconds $DwellSeconds

  # The injector runs as its own process on purpose. See inject-key.ps1 — doing
  # it inline detaches THIS shell from its console.
  # Its own stdout goes with the console it frees, so read the exit code rather
  # than expecting to see what it printed.
  $shellExe = (Get-Process -Id $PID).Path
  $injector = Join-Path $PSScriptRoot 'inject-key.ps1'
  & $shellExe -NoProfile -File $injector -ProcessId $recorder.Id -Keys $Keys
  if ($LASTEXITCODE -ne 0) { Write-Warning "key injection failed; the recorder will be killed instead of quitting" }

  if (-not $recorder.WaitForExit(20000)) {
    Write-Warning "the recorded program did not exit after '$Keys'; killing it. The cast will have no restore in it."
    try { $recorder.Kill() } catch { }
    $recorder.WaitForExit(5000) | Out-Null
  }
  if ($recorder.ExitCode -ne 0) { Write-Warning "recorder exit code $($recorder.ExitCode)" }
} else {
  Write-Host "recording in THIS terminal, at its current size. Drive the session, then quit the program to stop."
  & $powerSession rec -f -c $Command $Cast
  if ($LASTEXITCODE -ne 0) { Write-Warning "recorder exit code $LASTEXITCODE" }
}

if (-not (Test-Path $Cast)) { throw "no cast was written: $Cast" }
$header = Get-Content $Cast -TotalCount 1 | ConvertFrom-Json
$events = (Get-Content $Cast | Measure-Object -Line).Lines - 1
Write-Host ("cast: {0}  {1}x{2}, {3} events, {4} bytes" -f `
  $Cast, $header.width, $header.height, $events, (Get-Item $Cast).Length)

if ($Gif) {
  $agg = Resolve-Tool 'agg'
  $aggArgs = @()
  if ($Rows) { $aggArgs += @('--rows', $Rows) }
  if ($Cols) { $aggArgs += @('--cols', $Cols) }
  if ($Select) { $aggArgs += @('--select', $Select) }
  $aggArgs += @($Cast, $Gif)
  & $agg @aggArgs
  if ($LASTEXITCODE -ne 0) { throw "agg failed with exit code $LASTEXITCODE" }
  Write-Host ("gif:  {0}  {1} bytes" -f $Gif, (Get-Item $Gif).Length)
}
