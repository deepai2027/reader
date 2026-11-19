# Large-Scale & Performance Hardening Validation

## Benchmark protocol

- Record baseline and candidate commits, Go version, OS, CPU, RAM, storage, fixture seed and sizes, URL cardinality, pattern set, batch sizes, cache conditions, and exact commands.
- Run at least three repetitions per case on the same host. Report median throughput and the largest observed peak process memory for each case. Record Go heap and allocations as diagnostics. Exclude fixture generation and setup from timing, and verify expected output counters for every measured run.

## Numeric gates

Record numeric values after the baseline and before tuning. All values and passing candidate measurements are required for merge.

| Gate | Threshold to record | Pass condition |
| --- | --- | --- |
| Peak memory | Maximum peak process memory at the default and largest supported batch sizes | Every corresponding run stays within its cap, including mostly distinct URLs |
| Memory scaling | Maximum allowed absolute peak-memory increase across the size ladder at fixed batch size | Both repeated-URL and high-cardinality workloads stay within the allowed increase |
| Throughput | Minimum median bytes per second for each reference workload | Candidate meets each floor, including the high-cardinality multi-GB case |
| Regex scaling | Maximum elapsed-time growth between specified input lengths for the seeded-pattern cases | Matching and near-miss cases stay within the scaling limit |

Derive the limits from baseline variance and the resource budget for the documented benchmark host. If the baseline exposes memory growth with cardinality, set a bounded-memory target that requires correcting it.

## Correctness and merge checks

- Verify expected counts, record hashes, URL priority, exact per-file URL deduplication, long-line boundary behavior, and checkpoint ordering on deterministic fixtures. Verify resume and publisher failure behavior on affected paths.
- Confirm the documented BATCH_SIZE default and both bounds are accepted at startup and values outside the bounds are rejected.
- Run `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./internal/processing/...` on a host with race-detector support. Record the batch and checkpoint ownership review.
- Attach baseline and candidate results, numeric gate decisions, benchmark commands, and the BATCH_SIZE rationale to the implementation PR. Each gate and correctness check must pass before merge.

## Gate values established from the baseline

- Peak memory ceiling is 96 MiB per run across the reference matrix. Repeated-URL baseline peaks were about 40 MiB, leaving headroom for a fixed-size index and variation. The high-cardinality baseline exceeded this ceiling by a wide margin.
- The absolute difference between 2 and 3 GiB peak working sets at batch size 500 must be at most 24 MiB for each URL distribution. The distinct baseline grew by over 160 MiB.
- Median throughput must reach 165 MiB/s for repeated URLs and 155 MiB/s for distinct URLs in each reference case. These floors allow for disk indexing and measurement variance relative to default-batch baseline medians of roughly 210–219 MiB/s.
- For each seeded regex scenario, median time may grow by at most 24× from 16 to 256 KiB and 6× from 256 KiB to approximately 1 MiB. The approximately 1 MiB case must complete in at most 250 ms per operation. Compare the larger inputs separately from the 1 KiB input because the small-input execution path showed a different per-byte cost in the baseline.

## Reproduce the Windows benchmark

Use Go 1.26.11 on Windows amd64. The measured host was a Windows Server 2022 VM (build 20348) with an AMD EPYC-Rome CPU, 8 logical processors, 15.62 GiB RAM, and NTFS storage on the system volume. Keep fixtures and scratch on the same volume for both revisions. The runner fixes GOMAXPROCS=4 and GOGC=100, uses a fresh test process per run, and leaves the OS file cache in its natural state between runs. First passes can include cold reads. No fixture generation is timed.

From the repo root, choose an existing scratch directory with at least 12 GiB free. For the baseline, use a separate checkout at 735fc230026a61dcf1da8de3774d98a6bcce7fc7 and copy only internal/processing/pipeline/performance_test.go into that checkout. Compile both revisions with the same benchmark harness.

```powershell
$work = 'C:/temp/reader-phase3'
$runner = Resolve-Path './specs/2026-11-19-large-scale-performance-hardening/measure.ps1'
go test -c -o "$work/candidate.exe" ./internal/processing/pipeline
pwsh -NoProfile -File $runner -Repository . -Executable "$work/candidate.exe" -WorkDirectory $work -Label candidate
# In the baseline checkout after copying the harness
go test -c -o "$work/baseline.exe" ./internal/processing/pipeline
pwsh -NoProfile -File $runner -Repository . -Executable "$work/baseline.exe" -WorkDirectory $work -Label baseline
```

The generator uses sequential hexadecimal IDs, with ID zero repeated for the low-cardinality case. Each pair contains a valid synthetic record with a 640-byte path segment and an 8 KiB rejected line. The 2 GiB fixture has 241,101 record/invalid pairs, and the 3 GiB fixture has 361,652. Expect one unique URL in each repeated fixture and one per valid record in each distinct fixture. Peak working set comes from the OS high-water counter, polled every 100 ms. The Go heap measurement is taken at return before any forced GC.

For profile diagnostics, run from internal/processing/pipeline with the same work directory and environment. Profile runs are excluded from the result matrix.

```powershell
$env:GOMAXPROCS = '4'
$env:GOGC = '100'
$env:TEMP = $work
$env:TMP = $work
$env:READER_BENCH_FILE = "$work/fixture-3-1.txt"
$env:READER_BENCH_DISTINCT = '1'
$env:READER_BENCH_BATCH = '500'
& "$work/baseline.exe" '-test.run=^TestPipelinePerformance$' "-test.cpuprofile=$work/distinct.cpu" "-test.memprofile=$work/distinct.mem"
& "$work/baseline.exe" '-test.run=^$' '-test.bench=^BenchmarkSeededRegex$/./^near-miss$/^1048575$' '-test.benchtime=2s' "-test.cpuprofile=$work/long.cpu"
go tool pprof -top "$work/baseline.exe" "$work/distinct.cpu"
go tool pprof -top -inuse_space "$work/baseline.exe" "$work/distinct.mem"
go tool pprof -top "$work/baseline.exe" "$work/long.cpu"
```

## Review and profile findings

The baseline high-cardinality CPU profile attributed about 55% of sampled CPU time cumulatively to the regexp bit-state path. The near-limit near-miss profile attributed about 98% cumulatively to the regexp state machine. The Go heap at pipeline return and the process high-water marks showed the cost of retaining the per-file URL set. The candidate keeps RE2 and caches capture-group indices at compilation.

The pipeline uses a synchronous reader and one run state per invocation. The batch, regex selector, and URL index have one owner, and the synchronous publish calls finish before the checkpoint write. The URL index and sampling spool are created per run and have no shared mutable data. The Agent polling path calls the pipeline synchronously, and the stub store protects its map with a mutex. The PostgreSQL checkpoint adapter writes each checkpoint through one synchronous upsert. The review covers the single-file pipeline model. Cross-process coordination remains part of infrastructure validation.

## Automated checks

- Passed with Go 1.26.11 on Windows amd64. Commands run were `go build ./...`, `go vet ./...`, and `go test ./...`.
- Passed `go test -race ./internal/processing/... ./internal/orchestrate/...` with CGO enabled and the portable LLVM-MinGW 22.1.8 toolchain.
- The tests exercise spill membership, cleanup, spill creation errors, delivery failures, long-line boundaries, replay of non-seekable sources, sample read errors, resume with different batch sizes, normalized record output, and configuration bounds.


## Final benchmark results

All 25 numeric checks passed. The candidate was the final uncommitted implementation on top of baseline commit `735fc230026a61dcf1da8de3774d98a6bcce7fc7`. The baseline and candidate executable SHA-256 values were `3c2e29791627d17ea744e43ce9dc3ccf17d2610b95b0ed8f1a4231b97f2c5817` and `6c93a7dfaea8fb69d1b2303ed75e374c1716792ffa070609df0332f56516149e`, respectively.

Each paired value shows baseline → candidate. Peak working set is the maximum of three runs. Other pipeline measurements are medians.

| File | URLs | Batch | Peak MiB | Median MiB/s | Median lines/s | Heap at return (MiB) | Allocations (millions) |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 GiB | Repeated | 500 | 43.62 → 18.28 | 210.24 → 209.71 | 49501 → 49377 | 1.44 → 1.44 | 2.903 → 2.909 |
| 3 GiB | Repeated | 500 | 41.24 → 16.10 | 213.68 → 216.66 | 50312 → 51014 | 1.93 → 1.98 | 4.355 → 4.360 |
| 2 GiB | Distinct | 500 | 347.56 → 38.58 | 218.85 → 230.76 | 51528 → 54331 | 249.13 → 14.82 | 2.897 → 2.936 |
| 3 GiB | Distinct | 500 | 512.04 → 38.71 | 218.52 → 220.66 | 51450 → 51955 | 427.87 → 15.36 | 4.344 → 4.419 |
| 3 GiB | Repeated | 1000 | 41.04 → 26.93 | 217.74 → 223.72 | 51266 → 52675 | 7.24 → 6.28 | 4.350 → 4.355 |
| 3 GiB | Distinct | 1000 | 518.68 → 43.40 | 229.75 → 219.36 | 54095 → 51647 | 366.32 → 10.10 | 4.343 → 4.418 |
| 2 GiB | Distinct | 128 | 338.44 → 37.88 | 209.90 → 225.29 | 49421 → 53045 | 304.92 → 11.27 | 2.900 → 2.939 |

At batch size 500, the absolute difference in peak working set between 2 and 3 GiB was 2.18 MiB for repeated URLs and 0.13 MiB for distinct URLs. At 3 GiB and batch size 500, the distinct-URL case reduced peak working set by 92.4% and sustained 220.66 MiB/s.

The configured example remains 500. Measured throughput was comparable at 500 and 1,000, and 500 halves the maximum line count that can be replayed from a failed batch. The 1,000-line upper bound is the largest measured setting. The one-line lower bound supports operators who need the smallest replay unit and was verified in the recovery and configuration tests.

Seeded regex medians in milliseconds per operation, with final-build scaling factors.

| Case | 1 KiB | 16 KiB | 256 KiB | ≈1 MiB | Growth from 16 to 256 KiB | Growth from 256 KiB to ≈1 MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| match | 0.029 → 0.029 | 2.488 → 2.651 | 39.597 → 39.265 | 172.208 → 158.360 | 14.81× | 4.03× |
| near-miss | 0.097 → 0.093 | 2.420 → 2.528 | 40.675 → 40.122 | 168.280 → 150.084 | 15.87× | 3.74× |
| non-match | 0.100 → 0.092 | 2.460 → 2.624 | 42.009 → 37.691 | 170.523 → 157.368 | 14.37× | 4.18× |
