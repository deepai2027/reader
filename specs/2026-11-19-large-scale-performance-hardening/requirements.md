# Large-Scale & Performance Hardening Requirements

## Context

Phase 3 is the first open phase in [the roadmap](../roadmap.md). The [mission](../mission.md) requires memory-bounded streaming, deterministic records, single-file processing, and safe recovery. The [tech stack](../tech-stack.md) specifies Go's standard-library regexp engine with RE2 syntax and infrastructure-independent core tests. Use the Go version declared in go.mod.

## Scope and decisions

- Measure the core pipeline end to end with a local file source and bounded test doubles for progress and message delivery. Generate synthetic multi-GB files with repeated and high-cardinality URLs, mixed valid and invalid lines, and varied line lengths. Exercise every active seeded pattern using synthetic inputs.
- Measure peak process memory, Go heap, throughput, and allocations across file sizes and batch sizes. Hold batch size and line-length distribution constant when testing how memory scales with file size and URL cardinality.
- Make targeted fixes where measurements or the concurrency review reveal gaps. The per-file URL set currently grows with distinct URLs. Preserve exact per-file deduplication if its storage changes. Document and test the treatment of lines around the supported size limit.
- Preserve record hashes, output shapes, URL priority, counters, regex selection, and delivery-before-checkpoint ordering. Preserve the existing resume and replay contract and cover affected paths with regression tests.
- Establish numeric memory and throughput merge gates from a reproducible baseline before evaluating candidate changes. Document the measured BATCH_SIZE default and supported bounds in .env.example. Keep startup validation and the documented bounds consistent.
- Review ownership of batch state, URL deduplication, and checkpoint writes. Keep the current single-file processing model.
- Out of scope: new infrastructure adapters, full-stack performance tuning, Status API changes, log rotation, and regex pattern authoring.

## Implemented behavior

Regex selection counts matches one line at a time across the first 5,000 lines. Ties still favor the lowest pattern ID. Seekable sources replay from the beginning. Streaming sources replay a temporary copy of the sampled bytes, including bytes the scanner read ahead.

The 1 MiB scanner buffer limit includes line endings. A newline-terminated line with a 1 MiB minus 1 byte payload and a single LF is accepted. A payload of 1 MiB followed by LF fails the scan. CRLF uses one additional byte. The scanner error stops the run at its last saved checkpoint. Batch text stops accumulating at 8 MiB, with up to one accepted line of overshoot. On resume, the reader skips exactly the recorded line count even if the configured batch size changed. The stored regex and priority remain in use.

The exact URL index spills after 8,192 distinct URLs or 4 MiB of URL text, whichever buffered limit would be exceeded first. The spill keeps a fixed-size bucket directory in memory and stores complete URLs on disk. Full-string comparison resolves hash collisions. Size temporary storage for roughly 20 bytes of index metadata plus the normalized URL length per distinct URL and, for a non-seekable source, room for the sampled input. Use a disk-backed OS temporary directory. Scratch files live there and are removed on ordinary return, including errors. A hard kill can leave scratch files for the operator to remove after confirming Reader has stopped. A temporary storage error stops processing without advancing the affected batch checkpoint.

Deduplication applies per uninterrupted run. Resume uses a fresh index, so an already published unique URL can be emitted again after restart. The checkpoint still advances only after all publishes for the batch succeed. A publish already acknowledged before an incomplete batch can be replayed under the existing delivery contract.
