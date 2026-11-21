# End-to-End Hardening Plan

## 1. Establish the tagged integration baseline

- Run the existing MVP, Status API, and Log Rotation suites with isolated test resources. Verify Docker can pull the Testcontainers images and record which scenarios actually ran.
- Map every README §17 item to an existing check or a gap. Use §§14–16 to identify missing workflow assertions, including cross-process ownership, checkpoint recovery, and retention.
- Compile the regex patterns seeded by the migrations and verify expected captures and selection with representative synthetic fixtures.

## 2. Exercise the full Reader workflow

- Extend the MVP harness with the Reader agent-mode entry point and the authenticated Status API. Reuse the HTTP Agent stub and real PostgreSQL, MinIO, and RabbitMQ services.
- Cover idle and busy Agent responses, file registration, streaming download, mixed valid and invalid lines, two independent RabbitMQ outputs, final counters, local cleanup, and polling for the next file. Add a no-match fixture.
- Verify status during processing and after completion. Use bounded waits tied to observable state and isolate each scenario's database, objects, queues, and working directory.

## 3. Prove recovery and close coverage gaps

- Synchronize interruption at a committed checkpoint and during a later batch. Restart Reader against the same services and inspect the saved line, counters, regex, priority, and broker output.
- Exercise transient and exhausted delivery failures with the real broker, PostgreSQL checkpoint failure, and contention between Reader instances. Verify restart and progress using controlled failure points.
- Reuse the existing retention tests for active-file protection and retryable deletion. Fill remaining §17 gaps and run a representative Phase 3 regression after any fixes.

## 4. Verify deployment and document operation

- Run the standard Compose smoke test with isolated external Agent stub, MinIO, and RabbitMQ endpoints. Verify startup, migrations, authenticated status, and one completed file through the containerized Reader.
- Align `.env.example`, Compose, and the README with the verified startup procedure, test credentials and setup, integration commands, and teardown. Record merge evidence and track live Agent interoperability separately.
