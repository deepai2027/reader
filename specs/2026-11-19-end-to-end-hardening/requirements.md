# End-to-End Hardening Requirements

Final Phase Step G verifies the deployed Reader workflow against real PostgreSQL, MinIO, and RabbitMQ. Steps A–F supply the adapters, Status API, and retention behavior. The MVP suite previously timed out on a Testcontainers image pull, and the roadmap still lists the Compose smoke test as open.

## Scope

- Reuse Testcontainers for PostgreSQL, MinIO, and RabbitMQ and a deterministic HTTP Agent stub for the workflow suite. Use dedicated databases and buckets for existing adapter suites that take endpoints from the environment. Use synthetic fixtures and test-only credentials.
- Exercise the production agent-mode wiring and verify the README §14 workflow with the existing tagged suites and an end-to-end test. Map README §17 to passing checks and verify the architectural guarantees in §§15–16.
- Preserve single-file ownership and durable PostgreSQL progress. A restart reuses the persisted regex and priority, resumes after the committed line, and can replay the uncommitted batch within the existing downstream deduplication contract.
- Verify independent broker messages, normalized URLs, deterministic record hashes, and priority. Keep username and password fields exclusive to Full Record messages.
- Verify local working-copy cleanup, continued availability of the source in MinIO after normal processing, and source deletion through eligible Log Rotation.
- Update the documented configuration and deployment procedure. The standard Compose stack contains Reader and PostgreSQL only.

## Scope boundaries

The phase covers integrated behavior, targeted fixes for demonstrated gaps, and deployment documentation. Keep the established port interfaces and adapter architecture. Agent implementation, operation of external services, authoring production regex content, and downstream consumers are outside the phase. Track interoperability with the live Agent separately from the Step G merge gate.
