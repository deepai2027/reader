# Log Rotation Requirements

Final Phase Step F removes expired file records from PostgreSQL and their original objects from MinIO. README §12 defines the lifecycle. The mission prioritizes recoverable progress and reserves source-object deletion for Log Rotation.

- Expiry is one calendar month after registration, based on `file_management.created_at`. Use UTC and clamp to the last day of the target month when the original day does not exist there. A row is old enough at or after that instant. Apply this calendar rule in place of the README's 30-day example.
- Protect files in `reading` and any other state the processing workflow can still claim or resume. A cleanup worker must establish exclusive ownership before performing deletion. Processing and concurrent cleanup workers must respect that ownership.
- Keep the database row until MinIO confirms deletion or confirms the object is already absent. A transient or ambiguous MinIO failure leaves the row available for retry. Following a crash or database failure after object deletion, retry must be able to finalize the database row safely.
- Run automatically at a configurable `LOG_ROTATION_INTERVAL`. Bound each run, prevent overlapping runs in one process, and coordinate cleanup across replicas. Report outcomes and failures through structured logs without exposing credentials.
- Scope covers file records and their associated original MinIO objects. Full pipeline verification belongs to Step G.
