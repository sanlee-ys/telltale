<#
.SYNOPSIS
  Sample a process tree for hours, and report whether the tree drifted.

.DESCRIPTION
  This script is the instrument behind design.md §5.1. `telltale council` holds a
  process tree, which is the room plus one long-lived child for each persistent
  seat. `telltale events` and `telltale otel grok` are foreground listeners that
  an operator starts once and leaves. No gate in this repository measures what
  these modes cost after the first minute. A benchmark measures one call. It
  cannot measure whether an idle room increases its memory across a day. This
  script measures the second property.

  The script has three modes, because a soak has three shapes:

  - RESIDENCY (`-Id` or `-Name`) samples one process tree at an interval. It
    records working set, private bytes, handle count, live CPU and child count.
    Use this mode for a long-lived process.
  - PER-FIRE (`-FireCommand`) starts a short-lived process many times and
    records each run. `telltale statusline` starts one process for each prompt,
    so it has no residency to sample. Two properties can change: the cost of one
    fire, and the STABILITY of that cost across many fires. A sample of a
    process that lives 30 ms shows one arbitrary state.
  - SUMMARY (`-Summarize`) renders a finished JSONL file again. The samples are
    the artifact and the table is one view of them, so a later reader can plot
    the file or read a new table without a second soak.

  WHAT THE OUTPUT IS FOR. One memory figure proves nothing. At a single sample,
  an allocator that keeps its pages looks the same as a leak. The summary
  therefore prints min, median, p95 and max, and it also prints two trend
  figures across the arm. A constant workload with an increasing trend is the
  condition this script must find. A large but constant figure is a size, and it
  is not a leak. The script measures and does not judge: a verdict belongs in
  design.md, beside the conditions of the measurement.

  ONE CIM QUERY FOR EACH SAMPLE. `Get-CimInstance Win32_Process` returns every
  process with the five fields this script needs, and with the parent id that
  builds the tree. One sample therefore costs one query at any tree size. One
  `Get-Process` call for each pid would cost one call for each process, and it
  would still need the CIM query for the parent data.

  THE SCRIPT DOES NOT USE `Get-Counter`, which is the obvious tool. Two measured
  properties disqualify it. Its counter set names are LOCALIZED, so
  `\Process(*)\Working Set` does not resolve on a Windows installation in
  another language, and an instrument that fails on the operator's machine has
  no value. Its instances also carry process NAMES, so several `telltale.exe`
  processes arrive as `telltale`, `telltale#1` and `telltale#2`, with no stable
  relation to a pid and no parent data. A tree soak needs the parent data.
  `Win32_Process` is keyed on the pid, it reports the parent, and it is
  independent of the language.

  PID REUSE. Windows uses a process id again after a process exits, and a soak
  runs long enough to meet that condition. Each sample builds the tree again
  from the parent data, and it accepts a child only when the creation time of
  the child is not earlier than the creation time of the parent. The script
  checks the root more strictly. It records the creation time of the root at the
  start. If that creation time changes, the arm stops. The alternative is a
  silent measurement of a different process that received the same pid.

  CPU BELONGS TO THE LIVE SET, AND NOT TO THE LIFETIME OF THE TREE. `cpu_s` adds
  the kernel time and the user time of the processes that are alive AT THAT
  SAMPLE. When a child exits, its time leaves the total. `cpu_s` can therefore
  decrease, and a simple delta can become negative. Each sample carries
  `set_changed`, which is true when the pid set is different from the pid set of
  the previous sample. The summary excludes those intervals from the idle-CPU
  figure. It does not report a negative value, and it does not report an
  invented zero. A vendor process that starts for each turn makes this condition
  usual, and not exceptional.

  Windows PowerShell 5.1 is the target (ADR-002: Windows is the primary
  platform, and 5.1 is what a Windows 11 box has before anyone installs
  anything). No ternary, no null-coalescing, no `-AsHashtable`, and `-Depth` is
  passed to every `ConvertTo-Json` because 5.1 defaults it to 2 and silently
  truncates deeper objects. It runs unchanged on PowerShell 7.

.PARAMETER Id
  Process id to root the tree at. Residency mode.

.PARAMETER Name
  Process name to root the tree at, without `.exe`. Residency mode. It must
  match exactly one running process; several matches is an error rather than a
  guess, because picking one would produce a labelled measurement of an
  arbitrary process.

.PARAMETER FireCommand
  Executable to run repeatedly. Per-fire mode.

.PARAMETER FireArgs
  Arguments passed to `-FireCommand` on every fire.

.PARAMETER FireStdin
  File piped to the fire's stdin. `telltale statusline` reads its payload there.

.PARAMETER FireExpect
  Regex every fire's stdout must match. A crash is fast, so an arm with no such
  assertion reports a binary that broke halfway as an improvement. `ci.yml`
  makes the same check on its 15 samples.

.PARAMETER Fires
  How many times to fire. Per-fire mode.

.PARAMETER Summarize
  Re-render a finished JSONL and exit. Summary mode.

.PARAMETER Out
  JSONL path the samples append to. One JSON object per line, `schema_version`
  1. Both modes write to the same file shape with a different `kind`.

.PARAMETER Label
  Arm name recorded on every sample, so several arms can share a plot.

.PARAMETER IntervalSeconds
  Seconds between residency samples. Default 10.

.PARAMETER DurationMinutes
  How long the residency arm runs. Default 35.

.EXAMPLE
  # the idle event sink, on a non-default port and a redirected home
  $env:USERPROFILE = 'C:\soak\home'
  $p = Start-Process .\telltale.exe -ArgumentList 'events','-addr','127.0.0.1:14519' -PassThru
  .\tools\soak.ps1 -Id $p.Id -Label events-idle -Out soak-events.jsonl -DurationMinutes 35

.EXAMPLE
  # the statusline, which has no residency to sample. -FireExpect is not
  # optional in practice: it is the assertion that separates a fast render from
  # a fast crash, and `Opus` is the same marker ci.yml's 15-sample step asserts.
  .\tools\soak.ps1 -FireCommand .\telltale.exe -FireArgs 'statusline' `
      -FireStdin internal\statusline\testdata\full.json -FireExpect Opus `
      -Fires 1000 -Label statusline-fire -Out soak-statusline.jsonl

.EXAMPLE
  .\tools\soak.ps1 -Summarize soak-events.jsonl
#>

[CmdletBinding(DefaultParameterSetName = 'Residency')]
param(
    [Parameter(ParameterSetName = 'Residency')]
    [int] $Id,

    [Parameter(ParameterSetName = 'Residency')]
    [string] $Name,

    [Parameter(ParameterSetName = 'Fire', Mandatory = $true)]
    [string] $FireCommand,

    [Parameter(ParameterSetName = 'Fire')]
    [string[]] $FireArgs = @(),

    [Parameter(ParameterSetName = 'Fire')]
    [string] $FireStdin,

    [Parameter(ParameterSetName = 'Fire')]
    [string] $FireExpect,

    [Parameter(ParameterSetName = 'Fire')]
    [int] $Fires = 100,

    [Parameter(ParameterSetName = 'Summary', Mandatory = $true)]
    [string] $Summarize,

    [Parameter(ParameterSetName = 'Residency')]
    [Parameter(ParameterSetName = 'Fire')]
    [string] $Out,

    [Parameter(ParameterSetName = 'Residency')]
    [Parameter(ParameterSetName = 'Fire')]
    [string] $Label = 'arm',

    [Parameter(ParameterSetName = 'Residency')]
    [int] $IntervalSeconds = 10,

    [Parameter(ParameterSetName = 'Residency')]
    [int] $DurationMinutes = 35
)

$ErrorActionPreference = 'Stop'
$SchemaVersion = 1

# ---------------------------------------------------------------- writing

$script:Utf8NoBom = New-Object System.Text.UTF8Encoding($false)

function Write-Sample {
    param([string] $Path, $Record)

    $line = $Record | ConvertTo-Json -Depth 5 -Compress
    if ($Path) {
        [System.IO.File]::AppendAllText($Path, $line + "`r`n", $script:Utf8NoBom)
    } else {
        Write-Output $line
    }
}

# ---------------------------------------------------------------- statistics

function Get-Median {
    param([double[]] $Values)

    if ($Values.Count -eq 0) { return $null }
    $sorted = $Values | Sort-Object
    $mid = [int][math]::Floor($sorted.Count / 2)
    if ($sorted.Count % 2 -eq 1) { return $sorted[$mid] }
    return ($sorted[$mid - 1] + $sorted[$mid]) / 2
}

function Get-Percentile {
    <#
      Nearest-rank percentile. The distribution tail carries the information in
      a soak: one fire in 1000 took 3.6 s against a median of 31 ms. A maximum
      alone cannot show whether that was one sample or 100 samples. §7.18
      reports p50 and p95 for the same reason, so this summary matches it.
    #>
    param([double[]] $Values, [double] $P)

    if ($Values.Count -eq 0) { return $null }
    $sorted = $Values | Sort-Object
    $rank = [int][math]::Ceiling(($P / 100) * $sorted.Count) - 1
    if ($rank -lt 0) { $rank = 0 }
    if ($rank -ge $sorted.Count) { $rank = $sorted.Count - 1 }
    return $sorted[$rank]
}

function Get-HalfDrift {
    <#
      Median of the second half minus median of the first half.

      The least-squares slope beside it is the usual unit for a leak, and it has
      one bad property on this data: one outlier moves it. The first statusline
      arm fitted +32.4 ms for each 1000 fires, and ONE fire in 1000 that took
      3.6 s caused almost all of that figure. A reader would report a binary that
      drifts, from one delay in the scheduler. An outlier cannot move a median.
      When the two figures disagree, the distribution tail causes the slope, and
      not a trend. The script reports this figure with the slope, and never
      instead of the slope.
    #>
    param([double[]] $Values)

    if ($Values.Count -lt 6) { return $null }
    $half = [int][math]::Floor($Values.Count / 2)
    $first = Get-Median -Values ([double[]]$Values[0..($half - 1)])
    $second = Get-Median -Values ([double[]]$Values[$half..($Values.Count - 1)])
    return $second - $first
}

function Get-Slope {
    <#
      Least-squares slope of y over x, in y-units for each x-unit. The function
      returns $null for fewer than three points, and for a degenerate x span. A
      line through two samples shows no trend, and a report of one would be the
      estimate presented as a measurement that this repository refuses.
    #>
    param([double[]] $X, [double[]] $Y)

    $n = $X.Count
    if ($n -lt 3 -or $n -ne $Y.Count) { return $null }

    $meanX = ($X | Measure-Object -Average).Average
    $meanY = ($Y | Measure-Object -Average).Average
    $num = 0.0
    $den = 0.0
    for ($i = 0; $i -lt $n; $i++) {
        $dx = $X[$i] - $meanX
        $num += $dx * ($Y[$i] - $meanY)
        $den += $dx * $dx
    }
    if ($den -eq 0) { return $null }
    return $num / $den
}

function Format-Bytes {
    param($Bytes)

    if ($null -eq $Bytes) { return '--' }
    return '{0:0.00} MiB' -f ($Bytes / 1MB)
}

function Format-Drift {
    <#
      A drift figure with its label, or `--` when the arm had too few samples.

      `Get-HalfDrift` returns $null under six samples, and PowerShell evaluates
      `$null / 1MB` as 0. The first smoke arm therefore printed
      `peak ws 0.00 MiB` for a drift it never measured. That 0 is not a small
      drift. It is an absent figure wearing the appearance of a measured one,
      which is the zero-vs-absent collapse ADR-001 forbids, in the summary of
      the instrument that exists to find drift. The null test must happen BEFORE
      the arithmetic, so it happens here once and the four call sites cannot
      each get it wrong.
    #>
    param($Value, [string] $Label, [bool] $Bytes)

    if ($null -eq $Value) { return "$Label --" }
    if ($Bytes) { return '{0} {1:+0.00;-0.00;0.00} MiB' -f $Label, ($Value / 1MB) }
    return '{0} {1:+0.0;-0.0;0.0}' -f $Label, $Value
}

# ---------------------------------------------------------------- peak memory

function Initialize-PeakReader {
    <#
      A fire's peak working set, read from the handle after the process exits.

      `System.Diagnostics.Process.PeakWorkingSet64` is the obvious call, and it
      does not work here. After the process exits, that property reads a
      snapshot that no longer exists, and it returns 0. This is measured: the
      first per-fire arm reported `peak ws  0.00 MiB` for all six fires. That 0
      is not a small measurement. It is an absent measurement in the form of a
      number, which is the zero-vs-absent collapse that ADR-001 forbids.

      `GetProcessMemoryInfo` in psapi reads from the process HANDLE, which stays
      valid while this script holds it. It therefore reports the true peak of the
      run that just ended. When the call fails, the script records null, and the
      summary prints `--`. The script never substitutes 0.
    #>
    if ('TelltaleSoak.Mem' -as [type]) { return $true }
    try {
        # No -UsingNamespace: Add-Type already emits `using
        # System.Runtime.InteropServices;` for -MemberDefinition, and a second
        # one is a duplicate-using warning that its warnings-as-errors compile
        # turns into a hard failure.
        Add-Type -Namespace TelltaleSoak -Name Mem -MemberDefinition @'
[StructLayout(LayoutKind.Sequential)]
public struct COUNTERS {
    public uint cb;
    public uint PageFaultCount;
    public UIntPtr PeakWorkingSetSize;
    public UIntPtr WorkingSetSize;
    public UIntPtr QuotaPeakPagedPoolUsage;
    public UIntPtr QuotaPagedPoolUsage;
    public UIntPtr QuotaPeakNonPagedPoolUsage;
    public UIntPtr QuotaNonPagedPoolUsage;
    public UIntPtr PagefileUsage;
    public UIntPtr PeakPagefileUsage;
}
[DllImport("psapi.dll", SetLastError=true)]
public static extern bool GetProcessMemoryInfo(IntPtr handle, out COUNTERS counters, uint size);
'@
        return $true
    } catch {
        Write-Warning ("peak memory per fire is unavailable: {0}" -f $_.Exception.Message)
        return $false
    }
}

function Get-PeakMemory {
    param($Proc)

    $result = [pscustomobject]@{ ws = $null; priv = $null }
    if (-not ('TelltaleSoak.Mem' -as [type])) { return $result }
    try {
        $counters = New-Object 'TelltaleSoak.Mem+COUNTERS'
        $size = [System.Runtime.InteropServices.Marshal]::SizeOf($counters)
        if ([TelltaleSoak.Mem]::GetProcessMemoryInfo($Proc.Handle, [ref]$counters, $size)) {
            $result.ws = [long]$counters.PeakWorkingSetSize.ToUInt64()
            $result.priv = [long]$counters.PeakPagefileUsage.ToUInt64()
        }
    } catch {
        # Left null on purpose. See Initialize-PeakReader.
    }
    return $result
}

# ---------------------------------------------------------------- the tree

function Get-ProcessTable {
    <#
      Every process on the machine, keyed by pid. One query serves the whole
      sample: parentage, memory, handles and cpu all come from the same class.
    #>
    $table = @{}
    $all = Get-CimInstance -ClassName Win32_Process -Property `
        ProcessId, ParentProcessId, Name, WorkingSetSize, PrivatePageCount, HandleCount, `
        KernelModeTime, UserModeTime, CreationDate
    foreach ($p in $all) { $table[[int]$p.ProcessId] = $p }
    return $table
}

function Get-Tree {
    <#
      The root and every descendant, breadth first. A child counts only when its
      creation time is not earlier than its parent's -- the pid-reuse guard the
      header describes. An unreadable creation time is treated as failing the
      guard, because a child this cannot date is a child this cannot vouch for.
    #>
    param([hashtable] $Table, [int] $RootPid)

    if (-not $Table.ContainsKey($RootPid)) { return @() }

    $byParent = @{}
    foreach ($p in $Table.Values) {
        $parent = [int]$p.ParentProcessId
        if (-not $byParent.ContainsKey($parent)) { $byParent[$parent] = New-Object System.Collections.ArrayList }
        [void]$byParent[$parent].Add($p)
    }

    $tree = New-Object System.Collections.ArrayList
    $queue = New-Object System.Collections.Queue
    $queue.Enqueue($Table[$RootPid])
    while ($queue.Count -gt 0) {
        $current = $queue.Dequeue()
        [void]$tree.Add($current)
        $currentPid = [int]$current.ProcessId
        if (-not $byParent.ContainsKey($currentPid)) { continue }
        foreach ($child in $byParent[$currentPid]) {
            if ([int]$child.ProcessId -eq $currentPid) { continue }
            if ($null -eq $child.CreationDate -or $null -eq $current.CreationDate) { continue }
            if ($child.CreationDate -lt $current.CreationDate) { continue }
            $queue.Enqueue($child)
        }
    }
    return $tree.ToArray()
}

function Measure-Tree {
    param($Tree)

    # The loop counts the processes, and the function does not read
    # `$Tree.Count`. PowerShell converts a one-element array to a scalar on
    # return, and a tree of one process reported a count of 0. In the summary a
    # reader would read that 0 as an exit of the root process.
    $ws = 0; $priv = 0; $handles = 0; $cpu100ns = 0; $n = 0
    $procs = New-Object System.Collections.ArrayList
    foreach ($p in $Tree) {
        $n++
        $ws += [long]$p.WorkingSetSize
        $priv += [long]$p.PrivatePageCount
        $handles += [int]$p.HandleCount
        $t = [long]$p.KernelModeTime + [long]$p.UserModeTime
        $cpu100ns += $t
        [void]$procs.Add([pscustomobject]@{
            pid     = [int]$p.ProcessId
            name    = [string]$p.Name
            ws      = [long]$p.WorkingSetSize
            priv    = [long]$p.PrivatePageCount
            handles = [int]$p.HandleCount
            cpu_s   = [math]::Round($t / 1e7, 3)
        })
    }
    return [pscustomobject]@{
        procs   = $procs.ToArray()
        count   = $n
        ws      = $ws
        priv    = $priv
        handles = $handles
        cpu_s   = [math]::Round($cpu100ns / 1e7, 3)
    }
}

# ---------------------------------------------------------------- summary

function Show-Summary {
    param([string] $Path)

    $lines = Get-Content -Path $Path | Where-Object { $_.Trim().Length -gt 0 }
    if ($lines.Count -eq 0) { Write-Host "no samples in $Path"; return }

    $records = $lines | ForEach-Object { $_ | ConvertFrom-Json }
    $samples = @($records | Where-Object { $_.kind -eq 'sample' })
    $fires = @($records | Where-Object { $_.kind -eq 'fire' })
    $meta = @($records | Where-Object { $_.kind -eq 'meta' })

    foreach ($m in $meta) {
        Write-Host ''
        Write-Host ("arm {0} -- {1}" -f $m.label, $m.note)
        Write-Host ("  started {0}   host {1}   cpus {2}" -f $m.started, $m.host, $m.cpus)
    }

    if ($samples.Count -gt 0) {
        $elapsed = [double[]]@($samples | ForEach-Object { [double]$_.elapsed_s })
        $span = $elapsed[$elapsed.Count - 1] - $elapsed[0]
        Write-Host ''
        Write-Host ("residency: {0} samples over {1:0.0} min" -f $samples.Count, ($span / 60))
        # A slope for each hour, from an arm shorter than one hour, is an
        # extrapolation. A shorter arm amplifies the usual variation more. The
        # summary therefore prints the factor. Without it, a reader can read a
        # 65x amplification of a 0.03 MiB variation as a measured hourly leak.
        if ($span -gt 0 -and $span -lt 3600) {
            Write-Host ("  note: the arm ran {0:0.0} min, so every slope/hour below is a {1:0.0}x extrapolation, not a measured hourly figure" -f `
                ($span / 60), (3600 / $span))
        }
        Write-Host ''
        Write-Host ('  {0,-16} {1,12} {2,12} {3,12} {4,12} {5,16}' -f 'metric', 'min', 'median', 'p95', 'max', 'slope/hour')
        Write-Host ('  ' + ('-' * 88))

        $drifts = New-Object System.Collections.ArrayList
        $metrics = @(
            @{ key = 'ws';      label = 'working set';   bytes = $true },
            @{ key = 'priv';    label = 'private bytes'; bytes = $true },
            @{ key = 'handles'; label = 'handles';       bytes = $false },
            @{ key = 'count';   label = 'processes';     bytes = $false }
        )
        $hours = [double[]]@($elapsed | ForEach-Object { $_ / 3600 })
        foreach ($metric in $metrics) {
            $values = [double[]]@($samples | ForEach-Object { [double]$_.($metric.key) })
            $stats = $values | Measure-Object -Minimum -Maximum
            $median = Get-Median -Values $values
            $p95 = Get-Percentile -Values $values -P 95
            $slope = Get-Slope -X $hours -Y $values
            if ($metric.bytes) {
                $min = Format-Bytes $stats.Minimum
                $mid = Format-Bytes $median
                $hi = Format-Bytes $p95
                $max = Format-Bytes $stats.Maximum
                if ($null -eq $slope) { $trend = '--' } else { $trend = '{0:+0.00;-0.00;0.00} MiB' -f ($slope / 1MB) }
                [void]$drifts.Add((Format-Drift -Value (Get-HalfDrift -Values $values) -Label $metric.label -Bytes $true))
            } else {
                $min = '{0:0}' -f $stats.Minimum
                $mid = '{0:0.#}' -f $median
                $hi = '{0:0}' -f $p95
                $max = '{0:0}' -f $stats.Maximum
                if ($null -eq $slope) { $trend = '--' } else { $trend = '{0:+0.0;-0.0;0.0}' -f $slope }
                [void]$drifts.Add((Format-Drift -Value (Get-HalfDrift -Values $values) -Label $metric.label -Bytes $false))
            }
            Write-Host ('  {0,-16} {1,12} {2,12} {3,12} {4,12} {5,16}' -f $metric.label, $min, $mid, $hi, $max, $trend)
        }
        Write-Host ''
        Write-Host '  robust drift, median of the second half minus the first, which one outlier cannot move:'
        Write-Host ('    ' + ($drifts -join '   '))

        # Idle CPU, over intervals whose pid set did not change. A membership
        # change makes the delta incomparable (see the header); those intervals
        # are dropped and counted rather than reported.
        $cpuPcts = New-Object System.Collections.ArrayList
        $skipped = 0
        $cpus = 1
        if ($meta.Count -gt 0 -and $meta[0].cpus) { $cpus = [int]$meta[0].cpus }
        for ($i = 1; $i -lt $samples.Count; $i++) {
            if ($samples[$i].set_changed) { $skipped++; continue }
            $dt = [double]$samples[$i].elapsed_s - [double]$samples[$i - 1].elapsed_s
            $dcpu = [double]$samples[$i].cpu_s - [double]$samples[$i - 1].cpu_s
            if ($dt -le 0 -or $dcpu -lt 0) { $skipped++; continue }
            [void]$cpuPcts.Add(100 * $dcpu / ($dt * $cpus))
        }
        Write-Host ''
        if ($cpuPcts.Count -gt 0) {
            $arr = [double[]]$cpuPcts.ToArray()
            $stats = $arr | Measure-Object -Minimum -Maximum
            Write-Host ('  cpu over {0} comparable intervals ({1} dropped): min {2:0.000}%  median {3:0.000}%  max {4:0.000}%  total {5:0.00} s' -f `
                $arr.Count, $skipped, $stats.Minimum, (Get-Median -Values $arr), $stats.Maximum, `
                ([double]$samples[$samples.Count - 1].cpu_s))
        } else {
            Write-Host ('  cpu: no comparable interval ({0} dropped) -- the pid set changed on every sample' -f $skipped)
        }
    }

    if ($fires.Count -gt 0) {
        $ok = @($fires | Where-Object { $_.exit -eq 0 })
        Write-Host ''
        Write-Host ("per-fire: {0} fires, {1} exited 0" -f $fires.Count, $ok.Count)
        # Windows accounts process CPU time in scheduler ticks of 15.625 ms, so
        # a single fire's cpu ms is quantized to a multiple of that and a lone
        # 0.0 means "under one tick", not "no CPU". Only the distribution over
        # many fires carries information here; one reading carries +/- one tick.
        Write-Host '  note: cpu ms is quantized to the 15.625 ms scheduler tick, so read the distribution and never one fire'
        Write-Host ''
        Write-Host ('  {0,-16} {1,12} {2,12} {3,12} {4,12} {5,16}' -f 'metric', 'min', 'median', 'p95', 'max', 'slope/1k fires')
        Write-Host ('  ' + ('-' * 88))
        $drifts = New-Object System.Collections.ArrayList
        $fireMetrics = @(
            @{ key = 'wall_ms';   label = 'wall ms';      bytes = $false },
            @{ key = 'cpu_ms';    label = 'cpu ms';       bytes = $false },
            @{ key = 'peak_ws';   label = 'peak ws';      bytes = $true },
            @{ key = 'peak_priv'; label = 'peak private'; bytes = $true }
        )
        foreach ($metric in $fireMetrics) {
            # A null is dropped, never read as 0. A metric this arm could not
            # measure prints as absent rather than as a very good result.
            $present = @($fires | Where-Object { $null -ne $_.($metric.key) })
            if ($present.Count -eq 0) {
                Write-Host ('  {0,-16} {1,12} {2,12} {3,12} {4,12} {5,16}' -f $metric.label, '--', '--', '--', '--', '--')
                continue
            }
            $values = [double[]]@($present | ForEach-Object { [double]$_.($metric.key) })
            # x is the fire's own ordinal in thousands, so the slope reads as
            # "per 1000 fires" and stays right when some fires lack the metric.
            $index = [double[]]@($present | ForEach-Object { [double]$_.n / 1000 })
            $stats = $values | Measure-Object -Minimum -Maximum
            $median = Get-Median -Values $values
            $p95 = Get-Percentile -Values $values -P 95
            $slope = Get-Slope -X $index -Y $values
            if ($metric.bytes) {
                $min = Format-Bytes $stats.Minimum
                $mid = Format-Bytes $median
                $hi = Format-Bytes $p95
                $max = Format-Bytes $stats.Maximum
                if ($null -eq $slope) { $trend = '--' } else { $trend = '{0:+0.00;-0.00;0.00} MiB' -f ($slope / 1MB) }
            } else {
                $min = '{0:0.0}' -f $stats.Minimum
                $mid = '{0:0.0}' -f $median
                $hi = '{0:0.0}' -f $p95
                $max = '{0:0.0}' -f $stats.Maximum
                if ($null -eq $slope) { $trend = '--' } else { $trend = '{0:+0.0;-0.0;0.0}' -f $slope }
            }
            Write-Host ('  {0,-16} {1,12} {2,12} {3,12} {4,12} {5,16}' -f $metric.label, $min, $mid, $hi, $max, $trend)
            if ($metric.bytes) {
                [void]$drifts.Add((Format-Drift -Value (Get-HalfDrift -Values $values) -Label $metric.label -Bytes $true))
            } else {
                [void]$drifts.Add((Format-Drift -Value (Get-HalfDrift -Values $values) -Label $metric.label -Bytes $false))
            }
        }
        Write-Host ''
        Write-Host '  robust drift, median of the second half minus the first, which one outlier cannot move:'
        Write-Host ('    ' + ($drifts -join '   '))
    }
    Write-Host ''
}

# ---------------------------------------------------------------- modes

if ($PSCmdlet.ParameterSetName -eq 'Summary') {
    Show-Summary -Path $Summarize
    return
}

$startedAt = Get-Date
$cpuCount = [int]$env:NUMBER_OF_PROCESSORS
if ($cpuCount -lt 1) { $cpuCount = 1 }

if ($PSCmdlet.ParameterSetName -eq 'Fire') {
    $note = 'per-fire: {0} {1}' -f $FireCommand, ($FireArgs -join ' ')
    Write-Sample -Path $Out -Record ([pscustomobject]@{
        schema_version = $SchemaVersion
        kind           = 'meta'
        label          = $Label
        note           = $note
        started        = $startedAt.ToString('o')
        host           = $env:COMPUTERNAME
        cpus           = $cpuCount
        fires          = $Fires
    })

    # ProcessStartInfo rather than Start-Process, and stdin written rather than
    # redirected from a path, because this is the shape ci.yml's own 15-sample
    # timing step uses (design.md §5's 2026-08-16 amendment). Two instruments
    # timing the same binary two different ways produce two numbers nobody can
    # compare; this arm is the long version of that step, not a rival to it.
    [void](Initialize-PeakReader)
    $exe = (Resolve-Path $FireCommand).Path
    $payloadBytes = New-Object byte[] 0
    if ($FireStdin) {
        $payloadBytes = [System.Text.Encoding]::UTF8.GetBytes((Get-Content -Path $FireStdin -Raw))
    }

    # THE BOM TRAP. This instrument met it on the first smoke run, and the note
    # is here because a second diagnosis costs an hour. On a console at code
    # page 65001, which is this machine's console, `[Console]::InputEncoding` is
    # a UTF8Encoding WITH a 3-byte preamble. .NET builds the child's stdin
    # writer from that encoding and sets AutoFlush. The flush writes the
    # preamble when the script first reads `$proc.StandardInput`. The BOM
    # therefore arrives BEFORE the payload, even when the script writes the
    # payload to `BaseStream` as raw bytes. `telltale statusline` then refuses
    # the input: "invalid character 'ï' looking for beginning of value". A BOM
    # is not JSON.
    #
    # The fixture on disk carries no BOM (`7B 0D 0A`, which is `{`). The
    # 15-sample step in ci.yml never meets this condition, because PowerShell 7
    # gives it an encoding without a preamble. This is therefore an artifact of
    # the 5.1 harness, and it is NOT a telltale defect. The arm must not record
    # it as one. `$psi.StandardInputEncoding` is the direct fix, and 5.1's .NET
    # Framework does not have that property. The script therefore replaces the
    # console encoding for the duration of the arm, and restores it in
    # `finally`.
    $priorInputEncoding = $null
    try {
        $priorInputEncoding = [Console]::InputEncoding
        [Console]::InputEncoding = New-Object System.Text.UTF8Encoding($false)
    } catch {
        # No console attached (a scheduled task, a redirected host). Nothing to
        # swap and nothing to restore; the writer inherits a preamble-free
        # encoding in that case anyway.
        $priorInputEncoding = $null
    }

    try {
    for ($i = 0; $i -lt $Fires; $i++) {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $exe
        $psi.Arguments = ($FireArgs -join ' ')
        $psi.RedirectStandardInput = $true
        $psi.RedirectStandardOutput = $true
        $psi.UseShellExecute = $false

        $watch = [System.Diagnostics.Stopwatch]::StartNew()
        $proc = [System.Diagnostics.Process]::Start($psi)
        $proc.StandardInput.BaseStream.Write($payloadBytes, 0, $payloadBytes.Length)
        $proc.StandardInput.BaseStream.Flush()
        $proc.StandardInput.Close()
        $stdout = $proc.StandardOutput.ReadToEnd()
        $proc.WaitForExit()
        $watch.Stop()

        # A crash is fast. ci.yml asserts the line rendered on every sample for
        # exactly this reason, and a soak that ran 300 times would otherwise
        # report a binary that broke at fire 40 as a large improvement.
        if ($FireExpect -and $stdout -notmatch $FireExpect) {
            throw "fire $i rendered nothing matching /$FireExpect/: $stdout"
        }

        # Both figures come off the still-open handle, so they describe the run
        # that just ended rather than a sample caught at some arbitrary moment
        # inside a process that lives about 25 ms. TotalProcessorTime reads
        # correctly after exit; the peak does not, which is what Get-PeakMemory
        # is for.
        $peak = Get-PeakMemory -Proc $proc
        $cpuMs = $null
        try { $cpuMs = [math]::Round($proc.TotalProcessorTime.TotalMilliseconds, 2) } catch { $cpuMs = $null }

        Write-Sample -Path $Out -Record ([pscustomobject]@{
            schema_version = $SchemaVersion
            kind           = 'fire'
            label          = $Label
            n              = $i
            ts             = (Get-Date).ToString('o')
            wall_ms        = [math]::Round($watch.Elapsed.TotalMilliseconds, 2)
            cpu_ms         = $cpuMs
            peak_ws        = $peak.ws
            peak_priv      = $peak.priv
            exit           = $proc.ExitCode
            out_bytes      = $stdout.Length
        })
        $proc.Dispose()
    }
    } finally {
        if ($null -ne $priorInputEncoding) {
            try { [Console]::InputEncoding = $priorInputEncoding } catch { }
        }
    }

    if ($Out) { Show-Summary -Path $Out }
    return
}

# Residency.
if ($Id -le 0) {
    if (-not $Name) { throw 'residency mode needs -Id or -Name' }
    $matched = @(Get-Process -Name $Name -ErrorAction SilentlyContinue)
    if ($matched.Count -eq 0) { throw "no running process named $Name" }
    if ($matched.Count -gt 1) {
        throw ("$Name matches {0} processes ({1}) -- pass -Id, because picking one would label a measurement of an arbitrary process" -f `
            $matched.Count, (($matched | ForEach-Object { $_.Id }) -join ', '))
    }
    $Id = $matched[0].Id
}

$table = Get-ProcessTable
if (-not $table.ContainsKey($Id)) { throw "no process with pid $Id" }
$rootBirth = $table[$Id].CreationDate
$rootName = $table[$Id].Name

Write-Sample -Path $Out -Record ([pscustomobject]@{
    schema_version = $SchemaVersion
    kind           = 'meta'
    label          = $Label
    note           = 'residency: pid {0} ({1}), every {2}s for {3} min' -f $Id, $rootName, $IntervalSeconds, $DurationMinutes
    started        = $startedAt.ToString('o')
    host           = $env:COMPUTERNAME
    cpus           = $cpuCount
    root_pid       = $Id
    root_name      = $rootName
    interval_s     = $IntervalSeconds
})

Write-Host ("soak: watching pid {0} ({1}) every {2}s for {3} min -> {4}" -f $Id, $rootName, $IntervalSeconds, $DurationMinutes, $Out)

$deadline = $startedAt.AddMinutes($DurationMinutes)
$clock = [System.Diagnostics.Stopwatch]::StartNew()
$previousSet = $null
$ended = 'deadline'

while ((Get-Date) -lt $deadline) {
    $table = Get-ProcessTable
    if (-not $table.ContainsKey($Id)) { $ended = 'root exited'; break }
    if ($table[$Id].CreationDate -ne $rootBirth) { $ended = 'pid reused by another process'; break }

    $tree = @(Get-Tree -Table $table -RootPid $Id)
    $m = Measure-Tree -Tree $tree
    $set = (($m.procs | ForEach-Object { $_.pid } | Sort-Object) -join ',')

    Write-Sample -Path $Out -Record ([pscustomobject]@{
        schema_version = $SchemaVersion
        kind           = 'sample'
        label          = $Label
        ts             = (Get-Date).ToString('o')
        elapsed_s      = [math]::Round($clock.Elapsed.TotalSeconds, 2)
        count          = $m.count
        ws             = $m.ws
        priv           = $m.priv
        handles        = $m.handles
        cpu_s          = $m.cpu_s
        set_changed    = ($null -ne $previousSet -and $set -ne $previousSet)
        procs          = $m.procs
    })
    $previousSet = $set

    # Sleep until the next tick, and not for a constant interval. A slow sample
    # would otherwise delay every later sample across an arm of several hours.
    $next = $IntervalSeconds - ($clock.Elapsed.TotalSeconds % $IntervalSeconds)
    Start-Sleep -Milliseconds ([int]($next * 1000))
}

Write-Sample -Path $Out -Record ([pscustomobject]@{
    schema_version = $SchemaVersion
    kind           = 'end'
    label          = $Label
    ts             = (Get-Date).ToString('o')
    elapsed_s      = [math]::Round($clock.Elapsed.TotalSeconds, 2)
    reason         = $ended
})

Write-Host ("soak: {0} after {1:0.0} min" -f $ended, ($clock.Elapsed.TotalMinutes))
if ($Out) { Show-Summary -Path $Out }
