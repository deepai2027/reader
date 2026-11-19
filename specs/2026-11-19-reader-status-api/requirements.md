# Reader Status API Requirements

This spec covers Final Phase Step E in the [roadmap](../roadmap.md). The [mission](../mission.md) makes PostgreSQL the authoritative processing ledger, and the [tech stack](../tech-stack.md) specifies standard-library HTTP and plain SQL. README section 11 defines the Agent-facing JSON contract.

## Required behavior

- Accept POST /status with a JSON object containing a file_ids array.
- Authenticate every request to the endpoint using a key supplied through Reader configuration. Return HTTP 401 for a missing or invalid key.
- Return HTTP 200 with a files array. Each returned object contains file_id, state, lines_read, and lines_accepted. Map state, lines_read, and lines_accepted from the persisted status, current_line, and matched_count, respectively. Include both reading and done rows.
- Read status from PostgreSQL using a bulk, read-only query. Report committed checkpoint values. On lookup failure, return a server error without exposing database details.
- Log request outcomes with API keys excluded. Load the inbound credential through the existing environment configuration flow. Compose continues to run Reader and PostgreSQL.

## Implemented contract details

The Reader uses the following conventions. Agent interoperability still needs verification.

- Use X-API-Key, matching the existing Agent client convention, and the inbound READER_API_KEY already named in the README. READER_API_ADDR defaults to :8080.
- Return known IDs in request order, collapse duplicates to their first occurrence, and omit unknown IDs. Return an empty files array when all requested IDs are unknown.
- Return HTTP 400 for invalid JSON, a missing or empty file_ids array, blank identifiers, or more than 500 identifiers. Return HTTP 413 for request bodies larger than 64 KiB.

Phase 3 performance work, MinIO local staging, log rotation, and full infrastructure hardening are outside Step E.
