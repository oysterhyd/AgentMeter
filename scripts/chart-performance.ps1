param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [Parameter(Mandatory)][string]$Fixture,
    [string]$Output = "$PSScriptRoot/../artifacts/chart-performance/native-frames.json",
    [ValidateRange(1, 5)][int]$Repetitions = 3
)

$ErrorActionPreference = 'Stop'
$baselineExe = (Resolve-Path -LiteralPath $Baseline).Path
$candidateExe = (Resolve-Path -LiteralPath $Candidate).Path
$fixturePath = (Resolve-Path -LiteralPath $Fixture).Path
$outputPath = [IO.Path]::GetFullPath($Output)
$runRoot = Join-Path (Split-Path $outputPath) ('native-runs-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$runs = [Collections.Generic.List[object]]::new()

for ($round = 1; $round -le $Repetitions; $round++) {
    $cases = @(@('baseline', $baselineExe), @('candidate', $candidateExe))
    if ($round % 2 -eq 0) { [array]::Reverse($cases) }
    foreach ($case in $cases) {
        $runDir = Join-Path $runRoot "$($case[0])-$round"
        $profile = Join-Path $runDir 'profile'
        $capture = Join-Path $runDir 'capture'
        New-Item -ItemType Directory -Path $profile -Force | Out-Null
        Copy-Item -LiteralPath (Join-Path $fixturePath 'monitor.sqlite') -Destination $profile
        $arguments = @('--offline', '--data', "`"$profile`"", '--home', "`"$profile`"", '--smoke', '--capture-dir', "`"$capture`"")
        $frameLog = Join-Path $runDir 'frames.log'
        $appProcess = Start-Process -FilePath $case[1] -ArgumentList $arguments -WindowStyle Hidden -Environment @{ MONITOR_CHART_PROFILE = '1'; MYGO_FRAME_STATS = 'all' } -RedirectStandardError $frameLog -RedirectStandardOutput (Join-Path $runDir 'stdout.log') -PassThru
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $peakPrivate, $peakWorking = 0L, 0L
        try {
            while (!$appProcess.HasExited) {
                if ($watch.Elapsed.TotalSeconds -gt 90) { throw 'Native chart scenario timed out.' }
                $appProcess.Refresh()
                if ($appProcess.HasExited) { break }
                $peakPrivate = [Math]::Max($peakPrivate, $appProcess.PrivateMemorySize64)
                $peakWorking = [Math]::Max($peakWorking, $appProcess.WorkingSet64)
                Start-Sleep -Milliseconds 25
            }
            $appProcess.WaitForExit()
            if ($appProcess.ExitCode -ne 0) { throw "Native chart scenario exited $($appProcess.ExitCode)." }
            $result = Get-Content -LiteralPath (Join-Path $capture 'result.json') -Raw | ConvertFrom-Json
            if (!$result.native -or $result.error -ne '') { throw 'Native chart acceptance failed.' }
            $frames = [Collections.Generic.List[object]]::new()
            $stage = 'startup'
            foreach ($line in Get-Content -LiteralPath $frameLog) {
                if ($line -match 'monitor: chart-profile begin (\w+)') { $stage = $Matches[1] }
                elseif ($line -match 'monitor: chart-profile end') { $stage = 'between-pages' }
                elseif ($line -match 'frame (\d+) took ([\d.]+) ms: build ([\d.]+)(.*?), layout ([\d.]+), paint ([\d.]+), present ([\d.]+) \((.*?)\)') {
                    $frames.Add([pscustomobject]@{ stage = $stage; frame = [int]$Matches[1]; totalMs = [double]$Matches[2]; buildMs = [double]$Matches[3]; repaint = $Matches[4].Contains('painted again'); layoutMs = [double]$Matches[5]; paintMs = [double]$Matches[6]; presentMs = [double]$Matches[7]; backend = $Matches[8] })
                }
            }
            $runs.Add([pscustomobject]@{ variant = $case[0]; round = $round; passed = $true; peakPrivateBytes = $peakPrivate; peakWorkingSetBytes = $peakWorking; directory = $runDir; frames = $frames })
            Write-Host "$($case[0]) round ${round}: $($frames.Count) measured native frames."
        } finally {
            if (!$appProcess.HasExited) { Stop-Process -Id $appProcess.Id -Force }
            $appProcess.Dispose()
        }
    }
}
[pscustomobject]@{ scenario = 'Same isolated 100-day fixture; all-history five-page native smoke, normal animations, alternating runs; frame statistics include all frames in each page stage.'; repetitions = $Repetitions; runs = $runs } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $outputPath -Encoding utf8
Write-Host "Native chart frame report: $outputPath"
