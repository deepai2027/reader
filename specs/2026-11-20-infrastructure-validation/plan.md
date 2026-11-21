# Infrastructure Validation Plan

## 1. Establish the Docker baseline

- Select the Reader revision and use a Docker-capable CI runner or host. Record the revision, Go version, Docker and Compose versions, and service image versions.
- Run the existing tagged suites with Compose smoke enabled and the dedicated-service overrides unset. Capture evidence that Testcontainers provisioned PostgreSQL, RabbitMQ, and MinIO and that the Compose subtest ran.
- Diagnose any failed setup or assertion. Fix demonstrated Reader issues, add focused regression coverage where needed, and rerun the checks before moving to staging.

## 2. Prepare the staging Agent scenarios

- Coordinate with the Agent team to confirm the staging endpoint, API version, authorization, assignment ownership, and the documented behavior for unavailable work, busy responses, and reissue or reconciliation.
- Prepare an isolated Reader deployment with dedicated PostgreSQL, MinIO, and RabbitMQ resources. Reserve synthetic fixture IDs and a way to observe assignment responses, Reader status, committed checkpoints, and queue output.
- Agree on a deterministic way to interrupt Reader before its first durable registration and after a committed batch. Capture timing and state on both sides of the handoff.

## 3. Verify assignment and restart recovery

- Process a staging assignment end to end and record the Agent response, persisted priority and counters, broker output, and authenticated Status API result.
- Interrupt Reader after the Agent returns an assignment and before PostgreSQL registration. Restart and verify that the documented recovery path makes the assignment available to Reader and that processing completes.
- Interrupt a separate assignment after a known committed checkpoint. Restart with the same database and verify that Reader resumes the saved work before polling for a new assignment.
- Reproduce and fix any Reader defect established by the evidence. Record Agent-side dependencies with the Agent team and rerun affected cases on the final revision.

## 4. Record the closeout evidence

- Attach Docker and staging results, observed checkpoints and queue counts, and any focused regressions to the validation record. Keep the follow-up open until both sets of checks pass.
