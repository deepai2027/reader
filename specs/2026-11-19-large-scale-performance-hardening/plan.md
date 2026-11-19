# Large-Scale & Performance Hardening Plan

## 1. Build repeatable workloads

- Add a streaming fixture generator with a fixed seed and a bounded output sink that counts records and URLs without retaining messages.
- Define a size ladder with at least two multi-GB sizes. Include repeated-URL and mostly distinct-URL variants, valid and invalid lines, and long lines around the scanner limit.
- Build a regex benchmark matrix using active seeded patterns and generated matching, non-matching, and near-miss inputs at increasing lengths. Check that each seeded pattern compiles with the production engine.

## 2. Measure the baseline and record merge gates

- Run the matrix on one documented host with controlled disk and cache conditions. Capture peak process memory, live heap, allocations, elapsed time, bytes and lines per second, and output counters.
- Sweep BATCH_SIZE around the current default of 500. Profile representative high-cardinality and long-line runs to identify the main sources of memory and CPU use.
- Record numeric gates and the measurement protocol in validation.md before evaluating candidate fixes. Keep the workloads and protocol fixed for the comparison.

## 3. Address measured bottlenecks and review state ownership

- Fix memory growth or throughput problems identified in the baseline. If deduplication requires storage outside the heap, verify exact membership, cleanup, and failure behavior. If line handling changes, test counters, checkpoints, and boundary cases.
- Review ownership of batch buffers, the URL set, and checkpoint writes. Cover changed paths with focused regression tests and run the race detector where supported.

## 4. Tune defaults and assemble merge evidence

- Choose and enforce BATCH_SIZE bounds based on the sweep. Document the default, bounds, and trade-offs in .env.example.
- Repeat the benchmark matrix against the candidate, capture results alongside the baseline, and verify every gate in validation.md. Record the build, vet, test, and race results.

## Implementation status

All four groups are implemented on the feature branch. See [validation.md](validation.md) for the passing checks, baseline and candidate measurements, and reproduction commands.
