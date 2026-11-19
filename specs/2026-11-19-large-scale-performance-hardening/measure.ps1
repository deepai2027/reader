[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Repository,
    [Parameter(Mandatory)][string]$Executable,
    [Parameter(Mandatory)][string]$WorkDirectory,
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9_-]+$')][string]$Label
)

$ErrorActionPreference = 'Stop'
$exe = (Resolve-Path -LiteralPath $Executable).Path
$work = (Resolve-Path -LiteralPath $WorkDirectory).Path
$pipeline = Join-Path (Resolve-Path -LiteralPath $Repository).Path 'internal/processing/pipeline'
$summary = Join-Path $work "$Label-summary.txt"
if (Test-Path -LiteralPath $summary) {
    throw "Choose a fresh label. Results already exist in $summary"
}

$env:GOMAXPROCS = '4'
$env:GOGC = '100'
$env:TEMP = $work
$env:TMP = $work
Push-Location $pipeline
try {
    foreach ($gib in 2, 3) {
        foreach ($distinct in 0, 1) {
            $fixture = Join-Path $work "fixture-$gib-$distinct.txt"
            if (!(Test-Path -LiteralPath $fixture)) {
                $partial = "$fixture.$([guid]::NewGuid()).partial"
                try {
                    $env:READER_BENCH_GENERATE = $partial
                    $env:READER_BENCH_BYTES = [string]($gib * 1GB)
                    $env:READER_BENCH_DISTINCT = [string]$distinct
                    & $exe '-test.run=^TestGeneratePerformanceFixture$' '-test.v'
                    if ($LASTEXITCODE) { throw 'Fixture generation failed' }
                    Move-Item -LiteralPath $partial -Destination $fixture
                } finally {
                    if (Test-Path -LiteralPath $partial) {
                        Remove-Item -LiteralPath $partial
                    }
                }
            }
            if ((Get-Item -LiteralPath $fixture).Length -lt ($gib * 1GB)) {
                throw "Fixture is smaller than expected. Move it out of the work directory and rerun. $fixture"
            }
        }
    }
    Remove-Item Env:READER_BENCH_GENERATE -ErrorAction SilentlyContinue
    & $exe '-test.run=^$' '-test.bench=^BenchmarkSeededRegex$' '-test.benchtime=100ms' '-test.count=3' |
        Set-Content (Join-Path $work "$Label-regex.txt")
    if ($LASTEXITCODE) { throw 'Regex benchmark failed' }

    $cases = @(@(2,0,500), @(3,0,500), @(2,1,500), @(3,1,500), @(3,0,1000), @(3,1,1000), @(2,1,128))
    foreach ($case in $cases) {
        $gib, $distinct, $batch = $case
        $env:READER_BENCH_FILE = Join-Path $work "fixture-$gib-$distinct.txt"
        $env:READER_BENCH_DISTINCT = [string]$distinct
        $env:READER_BENCH_BATCH = [string]$batch
        foreach ($rep in 1..3) {
            $output = Join-Path $work "$Label-$gib-$distinct-$batch-$rep.txt"
            $arguments = '-test.run=^TestPipelinePerformance$', '-test.v'
            $proc = Start-Process $exe -ArgumentList $arguments -WorkingDirectory $pipeline -WindowStyle Hidden -PassThru -RedirectStandardOutput $output
            try {
                $peak = 0
                do {
                    $proc.Refresh()
                    $peak = [Math]::Max($peak, [long]$proc.PeakWorkingSet64)
                } while (!$proc.WaitForExit(100))
                if ($proc.ExitCode -ne 0) {
                    Get-Content -LiteralPath $output
                    throw 'Pipeline benchmark failed'
                }
                $metrics = (Select-String -LiteralPath $output -Pattern 'MiB/s=').Line.Trim()
                "$Label $gib GiB distinct=$distinct batch=$batch rep=$rep peakMiB=$([Math]::Round($peak/1MB,2)) $metrics" |
                    Tee-Object -FilePath $summary -Append
            } finally {
                $proc.Dispose()
            }
        }
    }
} finally {
    Pop-Location
}
