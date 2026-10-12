param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [Parameter(Mandatory)][string]$Fixture,
    [string]$Output = "$PSScriptRoot/../artifacts/gui-memory/process-memory.json",
    [ValidateRange(1, 10)][int]$Repetitions = 3,
    [int]$ExpectedCalls = 20000,
    [hashtable]$CandidateEnvironment = @{}
)

$ErrorActionPreference = 'Stop'
if (!('MonitorMemoryCounters' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public static class MonitorMemoryCounters {
    [StructLayout(LayoutKind.Sequential)]
    private struct Counters {
        public uint Size, Faults;
        public UIntPtr PeakWorkingSet, WorkingSet, PeakPagedPool, PagedPool, PeakNonPagedPool, NonPagedPool, Pagefile, PeakPagefile, PrivateUsage, PrivateWorkingSet;
        public ulong SharedCommit;
    }
    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool K32GetProcessMemoryInfo(IntPtr process, ref Counters counters, uint size);
    public static ulong PrivateWorkingSet(IntPtr handle) {
        var c = new Counters();
        c.Size = (uint)Marshal.SizeOf<Counters>();
        if (!K32GetProcessMemoryInfo(handle, ref c, c.Size)) throw new Win32Exception(Marshal.GetLastWin32Error());
        return c.PrivateWorkingSet.ToUInt64();
    }
}
'@
}
$baselineExe = (Resolve-Path -LiteralPath $Baseline).Path
$candidateExe = (Resolve-Path -LiteralPath $Candidate).Path
$fixturePath = (Resolve-Path -LiteralPath $Fixture).Path
$outputPath = [IO.Path]::GetFullPath($Output)
$runRoot = Join-Path (Split-Path $outputPath) ('runs-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$measurements = [Collections.Generic.List[object]]::new()

for ($round = 1; $round -le $Repetitions; $round++) {
    $cases = @(@('baseline', $baselineExe), @('candidate', $candidateExe))
    if ($round % 2 -eq 0) { [array]::Reverse($cases) }
    foreach ($case in $cases) {
        $runDir = Join-Path $runRoot "$($case[0])-$round"
        $profile = Join-Path $runDir 'profile'
        $capture = Join-Path $runDir 'capture'
        New-Item -ItemType Directory -Path $profile -Force | Out-Null
        Copy-Item -LiteralPath (Join-Path $fixturePath 'monitor.sqlite') -Destination $profile
        $arguments = @('--offline', '--data', "`"$profile`"", '--home', "`"$profile`"", '--smoke-memory', '--capture-dir', "`"$capture`"")
        $environment = @{ MONITOR_MEMORY_PROFILE = '0' }
        if ($case[0] -eq 'candidate') {
            foreach ($entry in $CandidateEnvironment.GetEnumerator()) { $environment[$entry.Key] = $entry.Value }
        }
        $appProcess = Start-Process -FilePath $case[1] -ArgumentList $arguments -WindowStyle Hidden -Environment $environment -PassThru
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $samples = [Collections.Generic.List[object]]::new()
        $stageFile = Join-Path $capture 'memory-stage.txt'
        try {
            while (!$appProcess.HasExited) {
                if ($watch.Elapsed.TotalSeconds -gt 120) { throw 'GUI memory scenario timed out.' }
                $appProcess.Refresh()
                if ($appProcess.HasExited) { break }
                $stage = if (Test-Path -LiteralPath $stageFile) { Get-Content -LiteralPath $stageFile -Raw } else { 'startup' }
                $samples.Add([pscustomobject]@{
                    elapsedMs = $watch.ElapsedMilliseconds
                    stage = $stage
                    privateBytes = $appProcess.PrivateMemorySize64
                    privateWorkingSetBytes = [MonitorMemoryCounters]::PrivateWorkingSet($appProcess.Handle)
                    workingSetBytes = $appProcess.WorkingSet64
                    peakWorkingSetBytes = $appProcess.PeakWorkingSet64
                    cpuMs = $appProcess.TotalProcessorTime.TotalMilliseconds
                })
                Start-Sleep -Milliseconds 100
            }
            $appProcess.WaitForExit()
            if ($appProcess.ExitCode -ne 0) { throw "Native process exited $($appProcess.ExitCode)." }
            $result = Get-Content -LiteralPath (Join-Path $capture 'memory-result.json') -Raw | ConvertFrom-Json
            if ($result.error -ne '' -or !$result.verified) { throw "GUI memory scenario failed: $($result.error)" }
            $calls = Get-Content -LiteralPath (Join-Path $capture 'memory-calls.json') -Raw | ConvertFrom-Json
            if ($calls.pagination.total -ne $ExpectedCalls -or $calls.rows -ne [math]::Min(50, $ExpectedCalls)) { throw 'Call workload does not match the requested fixture.' }
            $stages = [ordered]@{}
            foreach ($group in ($samples | Group-Object stage)) {
                # Discard transitions; use the last two seconds of each state.
                $end = $group.Group[-1].elapsedMs
                $settled = @($group.Group | Where-Object { $_.elapsedMs -ge $end - 2000 })
                $private = @($settled.privateBytes | Sort-Object)
                $workingSet = @($settled.workingSetBytes | Sort-Object)
                $privateWorkingSet = @($settled.privateWorkingSetBytes | Sort-Object)
                $stages[$group.Name] = [pscustomobject]@{
                    medianPrivateBytes = $private[[int][math]::Floor($private.Count / 2)]
                    medianWorkingSetBytes = $workingSet[[int][math]::Floor($workingSet.Count / 2)]
                    medianPrivateWorkingSetBytes = $privateWorkingSet[[int][math]::Floor($privateWorkingSet.Count / 2)]
                    sampleCount = $settled.Count
                }
            }
            $measurement = [pscustomobject]@{
                build = $case[0]
                round = $round
                durationMs = $watch.ElapsedMilliseconds
                sampledPeakPrivateBytes = ($samples.privateBytes | Measure-Object -Maximum).Maximum
                sampledPeakPrivateWorkingSetBytes = ($samples.privateWorkingSetBytes | Measure-Object -Maximum).Maximum
                peakWorkingSetBytes = ($samples.peakWorkingSetBytes | Measure-Object -Maximum).Maximum
                cpuMs = $samples[-1].cpuMs
                stages = $stages
                heap = $result.checkpoints
                transitions = $result.transitions
                samples = $samples
            }
            $measurements.Add($measurement)
            $measurement | Select-Object build, round, sampledPeakPrivateBytes, peakWorkingSetBytes, cpuMs, stages | ConvertTo-Json -Depth 5 -Compress
        } finally {
            if (!$appProcess.HasExited) { $appProcess.Kill(); $appProcess.WaitForExit() }
            $appProcess.Dispose()
        }
    }
}

[pscustomobject]@{
    scenario = "$ExpectedCalls calls; real native overview, five pages, calls, card, orb, tray and restore; four seconds per state; no screenshots or diagnostic forced GC."
    samplingIntervalMs = 100
    candidateEnvironment = $CandidateEnvironment
    measurements = $measurements
} | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $outputPath -Encoding utf8
