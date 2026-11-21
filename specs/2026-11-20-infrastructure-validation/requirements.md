# Infrastructure Validation Requirements

This follow-up closes the validation items recorded after Step G. Step G passed against disposable local PostgreSQL, MinIO, and RabbitMQ with an HTTP Agent stub. Docker-backed integration, the deployment smoke test, and interoperability with a live Agent still require evidence.

- Run the existing Testcontainers suites and exercise the standard Reader and PostgreSQL Compose deployment. RabbitMQ, MinIO, and the HTTP Agent stub stay external to the standard Compose services.
- Verify interoperability against an authorized staging Agent with synthetic assignments and isolated test resources. Capture expected polling behavior, file ID and priority, authenticated status, and recovery across both the assignment-to-registration boundary and a later committed checkpoint.
- Preserve Reader's recovery contract. Resume saved work before requesting a new assignment, keep the saved regex and priority, account for all valid records, and limit any repeated delivery to uncommitted work.
- Limit changes to reproducible Reader defects uncovered by validation. Agent implementation, production deployment, and regex catalog verification are outside scope. If recovery depends on an unsupported Agent behavior, record the dependency and keep the staging gate open.
- Record evidence without staging credentials or payloads that contain sensitive data.
