# Log Rotation Plan

1. **PostgreSQL eligibility and ownership**
   - Calculate expiry from `file_management.created_at` and scan eligible rows in bounded batches.
   - Coordinate cleanup with processing claims and other cleanup workers. Recheck eligibility when acquiring ownership and protect ownership through object deletion and ledger finalization.
2. **MinIO deletion and retry**
   - Use the existing file-to-object mapping. Delete the MinIO object before deleting the PostgreSQL row.
   - Keep failed cleanup retryable. Handle a missing object as successful removal when the storage response confirms absence, so a retry can finish after an interrupted database update.
3. **Scheduler configuration and verification**
   - Wire the job into Reader startup and shutdown. Validate `LOG_ROTATION_INTERVAL`, document calendar-month retention, and reconcile the README's day-based setting example.
   - Add eligibility and retry tests, dedicated PostgreSQL and MinIO integration coverage, and a scheduled-run deployment smoke procedure. Record results in `validation.md`.
