param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [Parameter(Mandatory)][string]$Fixture,
    [string]$Output = "$PSScriptRoot/../artifacts/performance-2.8.2/process-memory.json",
    [ValidateRange(1, 10)][int]$Repetitions = 3
)

$ErrorActionPreference = 'Stop'
$baselineExe = (Resolve-Path -LiteralPath $Baseline).Path
$candidateExe = (Resolve-Path -LiteralPath $Candidate).Path
$fixturePath = (Resolve-Path -LiteralPath $Fixture).Path
$outputPath = [IO.Path]::GetFullPath($Output)
$runRoot = Join-Path (Split-Path $outputPath) ('memory-runs-' + [guid]::NewGuid().ToString('N'))
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
        $arguments = @('--offline', '--data', "`"$profile`"", '--home', "`"$profile`"", '--smoke', '--smoke-widget', '--capture-dir', "`"$capture`"")
        $appProcess = Start-Process -FilePath $case[1] -ArgumentList $arguments -WindowStyle Hidden -PassThru
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $samples = [Collections.Generic.List[object]]::new()
        try {
            while (!$appProcess.HasExited) {
                if ($watch.Elapsed.TotalSeconds -gt 120) { throw 'Native memory scenario timed out.' }
                $appProcess.Refresh()
                if ($appProcess.HasExited) { break }
                $samples.Add([pscustomobject]@{
                    elapsedMs = $watch.ElapsedMilliseconds
                    privateBytes = $appProcess.PrivateMemorySize64
                    workingSetBytes = $appProcess.WorkingSet64
                    peakWorkingSetBytes = $appProcess.PeakWorkingSet64
                    cpuMs = $appProcess.TotalProcessorTime.TotalMilliseconds
                })
                Start-Sleep -Milliseconds 100
            }
            $appProcess.WaitForExit()
            if ($appProcess.ExitCode -ne 0) { throw "Native process exited $($appProcess.ExitCode)." }
            $result = Get-Content -LiteralPath (Join-Path $capture 'result.json') -Raw | ConvertFrom-Json
            if ($result.error -ne '' -or !$result.native -or !$result.widgetVerified) { throw 'Native memory scenario failed acceptance.' }
            $calls = Get-Content -LiteralPath (Join-Path $capture 'calls-result.json') -Raw | ConvertFrom-Json
            if (!$calls.verified -or $calls.pagination.total -ne 20000 -or $calls.rows -ne 50) { throw 'Full-history call scenario failed acceptance.' }
            $measurement = [pscustomobject]@{
                build = $case[0]
                round = $round
                durationMs = $watch.ElapsedMilliseconds
                sampledPeakPrivateBytes = ($samples.privateBytes | Measure-Object -Maximum).Maximum
                peakWorkingSetBytes = ($samples.peakWorkingSetBytes | Measure-Object -Maximum).Maximum
                cpuMs = $samples[-1].cpuMs
                sampleCount = $samples.Count
                samples = $samples
            }
            $measurements.Add($measurement)
            $measurement | Select-Object build, round, sampledPeakPrivateBytes, peakWorkingSetBytes, cpuMs | ConvertTo-Json -Compress
        } finally {
            if (!$appProcess.HasExited) { $appProcess.Kill(); $appProcess.WaitForExit() }
            $appProcess.Dispose()
        }
    }
}

[pscustomobject]@{
    scenario = 'Same synthetic 20,000-call database; five real native pages plus all-history calls, card/orb interactions and window switching; offline.'
    samplingIntervalMs = 100
    measurements = $measurements
} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $outputPath -Encoding utf8
