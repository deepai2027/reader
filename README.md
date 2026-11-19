# Reader Module

## Overview

The **Reader** module is responsible for receiving file information from the Agent, downloading files from MinIO, processing large text files, identifying records using predefined Regular Expressions (Regex), extracting and validating information, and publishing processed data to RabbitMQ.

The module provides the following capabilities:

- Efficient processing of large text files using batch processing.
- Automatic Regex identification and selection.
- Data extraction and validation.
- Publishing extracted records to RabbitMQ.
- Processing only one file at a time.
- Resuming interrupted processing from the last saved position.
- Preventing duplicate processing after application restarts.
- Managing file processing priorities.
- Providing an API for reporting file processing statuses to the Agent.
- Managing data retention and file deletion through Log Rotation.

The module uses PostgreSQL for persistent storage and supports configurable runtime settings through environment variables.

---

## Technologies Used

The technologies used in the development of this module are:

- **Programming Language:** Go (Golang)
- **Database:** PostgreSQL
- **Message Broker:** RabbitMQ
- **Object Storage:** MinIO
- **Containerization:** Docker and Docker Compose
- **Configuration Management:** Environment Variables
- **Logging:** Standard structured logging
- **Testing:** Go standard testing package (`testing`)

**Important:** When running Docker Compose, this project must start only the Reader and PostgreSQL containers. All other services (RabbitMQ and MinIO) are already available in the existing infrastructure and must not be started by this project's Docker Compose configuration.

---

## 1. Database Architecture

The Reader module must use **PostgreSQL** as its primary database.

### 1.1. Regex Table

This table stores predefined Regex patterns used to identify and extract information from text files.

| Field | Description |
|---|---|
| `id` | Unique record identifier. |
| `name` | Regex name (maximum 100 characters). |
| `regex` | Stored Regex pattern. |
| `isActive` | Indicates whether the Regex is enabled or disabled. |

#### Requirements

- Regex patterns must be predefined and static.
- All predefined Regex records must be automatically inserted into the database during migrations.
- Only active Regex patterns can be used during file identification and processing.
- Disabled Regex patterns must be excluded from matching operations.

### 1.2. File Management Table

This table manages file processing states and stores processing progress information.

| Field | Description |
|---|---|
| `id` | Unique record identifier. |
| `file_id` | File identifier stored in MinIO. |
| `status` | File processing status: `reading` or `done`. |
| `current_line` | Number of the last processed line. |
| `matched_count` | Number of lines matched by the selected Regex. |
| `selected_regex_id` | ID of the Regex selected for processing the file. |
| `priority` | File priority received from the Agent. |
| `created_at` | Timestamp when the file record was created. |
| `updated_at` | Timestamp of the last record update. |

#### Requirements

- Processing progress must be persisted in the database.
- The Reader must resume processing from the last saved position after a shutdown or restart.
- This table is the primary source for file processing status APIs and Log Rotation operations.

---

## 2. Communication with Agent

The Agent is responsible for retrieving files from external sources and uploading them to MinIO.

After uploading a file:

1. The Agent stores the file in MinIO.
2. The Agent records the file information in its own database.
3. The Agent stores the MinIO file identifier and its priority.
4. The Reader periodically requests new files through an API provided by the Agent.

### 2.1. Agent API Configuration

The following configuration values must be loaded from the `.env` file:

- Agent API server address.
- Agent API port.
- Agent API Key.

### 2.2. File Retrieval Mechanism

The Reader must periodically call the Agent API to check whether a new file is available.

- The polling interval must be configurable through environment variables.
- If no new file is available, the Reader must wait for the configured interval before making another request.
- If a file is available, the Reader receives its information and starts the processing workflow.

The Agent response must include:

| Field | Description |
|---|---|
| `file_id` | MinIO file identifier. |
| `priority` | File processing priority. |

**Note:** Implementing and managing the Agent API is outside the scope of the Reader module. The Reader is only responsible for communicating with the API, sending requests, receiving responses, and managing the received information.

---

## 3. File Reception and Processing Management

The Reader must process only **one file at a time**.

When a new file is received from the Agent, the Reader must:

1. Receive the MinIO `file_id`.
2. Receive the file `priority`.
3. Save the file information in PostgreSQL.
4. Download the file from MinIO.
5. Update the file status to `reading`.
6. Start the file processing workflow.

### 3.1. Preventing Concurrent File Processing

While a file is being processed, the Reader must not call the Agent API to request another file.

If a new file retrieval request is received while another file is being processed, the Agent must return:

| Field | Value |
|---|---|
| HTTP Status Code | `409 Conflict` |
| Error Code | `file_in_reading` |
| Response Data | Information about the currently processing file. |

This mechanism prevents the Reader from receiving a new file before completing the current one.

---

## 4. Regex Selection and File Identification

After downloading a file, the Reader must inspect the first **5,000 lines** to identify the appropriate Regex pattern.

### 4.1. Regex Matching Process

1. Read the first 5,000 lines of the downloaded file.
2. Retrieve all active Regex patterns from the database.
3. Test each active Regex against the initial file content.
4. Identify Regex patterns capable of extracting the expected information.
5. If multiple Regex patterns match, select one according to the predefined selection logic.
6. Save the selected Regex ID in the `selected_regex_id` field of the File Management table.
7. Use the selected Regex for the remaining file processing.

The selected Regex must remain unchanged throughout the processing of that file.

### 4.2. Behavior When No Regex Matches

If none of the active Regex patterns matches the first 5,000 lines:

- The file must not enter the main processing stage.
- The file status must be updated to `done`.
- `matched_count` must be set to `0`.
- `current_line` must be set to `0`.
- The downloaded file must be deleted from the Reader's local directory.

**Important:** At this stage, the file must only be deleted from the local Reader directory. The original file stored in MinIO must remain untouched.

Files stored in MinIO can only be deleted through the Log Rotation mechanism.

---

## 5. Batch File Processing

Since the Reader is expected to process large text files, loading an entire file into memory is not allowed.

The Reader must implement line-based batch processing.

### 5.1. Batch Configuration

- Files must be read in batches containing a configurable number of lines.
- The batch size must be configurable through the `.env` file.
- The Reader must process each batch independently.
- Memory consumption must remain controlled regardless of the total file size.

### 5.2. Processing Each Batch

For each batch, the Reader must:

1. Read the next configured number of lines.
2. Apply the selected Regex to each line.
3. Extract the required information.
4. Validate extracted records.
5. Store valid records in the current batch array.
6. Publish the processed records to the appropriate RabbitMQ topics.
7. Update processing progress in PostgreSQL.

After each batch, the following information must be persisted:

- `current_line`: Number of the last processed line.
- `matched_count`: Total number of matched lines.
- `status`: Current file processing status.

### 5.3. Batch Publishing

Extracted records must be collected in an array during batch processing.

After completing each batch:

- The array must be published to the appropriate RabbitMQ topics.
- Each record must be published as an independent RabbitMQ message.
- Multiple records must not be combined into a single JSON message.

This approach ensures efficient memory usage and supports processing very large files.

---

## 6. Processing Recovery After Restart

The Reader must support automatic processing recovery.

If the application shuts down or restarts for any reason, it must first check the PostgreSQL database before requesting a new file from the Agent.

### 6.1. Recovery Workflow

If a file with the `reading` status exists, the Reader must resume processing that file.

The recovery process must follow these steps:

1. Retrieve the file identifier from the database.
2. Open the corresponding file from the local Reader directory.
3. Retrieve the last processed line number from `current_line`.
4. Retrieve the selected Regex ID from `selected_regex_id`.
5. Retrieve the file priority from `priority`.
6. Continue processing from the line immediately following the last processed line.
7. Continue processing until the file is completely processed.
8. Do not request another file from the Agent until the current file is completed.

### 6.2. Recovery Requirements

During recovery:

- The file must not be downloaded again from MinIO.
- Processing must not restart from the beginning of the file.
- Previously processed lines must not be processed again.
- The previously selected Regex must be reused.
- The original file priority must be preserved.

**Important:** The Reader must retain the local downloaded file until processing is completed successfully.

---

## 7. Data Extraction and Validation

The structure of each text file is determined by its corresponding Regex pattern.

There is no fixed file format or predefined delimiter such as `:` for separating information.

All extraction operations must be performed according to the selected Regex.

### 7.1. Extracted Information

For each matching line, the Reader must extract:

| Field | Description |
|---|---|
| `URL` | URL associated with the record. |
| `Username` | Extracted username. |
| `Password` | Extracted password. |

### 7.2. Data Validation

After extraction, the Reader must validate the extracted information.

Validation requirements include:

- The username must have a valid structure.
- The password must have a valid structure.
- Username and password lengths must fall within their configured allowed ranges.
- All required fields must match the expected structure defined by the selected Regex.
- The extracted URL must be valid.
- The URL must be normalized before further processing.

### 7.3. Invalid Records

If a line does not match the selected Regex or its extracted information fails validation:

- The line must be ignored.
- No extracted information from that line must be forwarded to subsequent processing stages.

Only valid records can be added to the current batch array.

### 7.4. Publishing Valid Records

After extraction and validation:

1. Add each valid record to the current batch array.
2. Complete the processing of the current batch.
3. Prepare the corresponding RabbitMQ messages.
4. Publish the messages to their respective topics.

Each valid record must be published as an independent RabbitMQ message.

---

## 8. Extracted Record Structure

For each valid extracted record, the Reader must generate the following information:

| Field | Description |
|---|---|
| `record_hash` | Hash generated from the URL, username, and password. |
| `username` | Extracted username. |
| `password` | Extracted password. |
| `url` | Normalized extracted URL. |
| `domain` | Main domain extracted from the URL. |
| `subdomain` | URL subdomain, if available. |
| `suffix` | Domain suffix. |
| `port` | URL port, if available. |
| `modified_date` | File's last modification date obtained from its metadata. |
| `created_at` | Record creation timestamp. |

### 8.1. Record Hash Generation

The `record_hash` must be generated using the following formula:

```text
hash(url|user|password)
```

The URL, username, and password must be separated by the pipe (`|`) character.

**Important:** The URL must be normalized before generating the hash to ensure consistency across the entire system.

---

## 9. RabbitMQ Integration

After extracting and validating records, the Reader must publish the processed information to RabbitMQ.

Two separate topics are required.

### 9.1. Full Record Topic

This topic receives complete extracted records.

Each record must contain:

- `record_hash`
- `username`
- `password`
- `url`
- `domain`
- `subdomain`
- `suffix`
- `port`
- `modified_date`
- `created_at`

#### Publishing Requirements

- All valid records from the current batch must be prepared as JSON objects.
- Each JSON object must be published as an independent RabbitMQ message.
- Multiple records must not be combined into one message.
- Messages must be published using batch publishing to improve throughput.
- The URL included in each record must already be normalized.

### 9.2. Unique URL Topic

The second topic is responsible for receiving unique URLs.

The Reader must:

1. Extract URLs from valid records.
2. Normalize all extracted URLs.
3. Remove duplicate URLs.
4. Associate each unique URL with the priority of its source file.
5. Publish each unique URL as an independent RabbitMQ message.

Each message must contain:

| Field | Description |
|---|---|
| `url` | Normalized and unique URL. |
| `priority` | Priority of the source file. |
| `domain` | Main domain. |
| `subdomain` | Subdomain, if available. |
| `suffix` | Domain suffix. |

**Important:**

- Username and password must never be included in this topic.
- URL priority must be inherited from the source file.
- URLs extracted from higher-priority files must be published with the corresponding higher priority.

### 9.3. RabbitMQ Priority Queue Configuration

RabbitMQ priority queue functionality must be enabled.

The priority configuration must be consistent with the priority values defined by the Agent.

This ensures that URLs extracted from higher-priority files are processed with higher priority by RabbitMQ.

---

## 10. File Processing Completion

After processing all lines of a file, the Reader must perform the finalization steps.

### 10.1. Finalization Workflow

1. Complete processing of all file batches.
2. Publish all extracted records to the appropriate RabbitMQ topics.
3. Update the file status in PostgreSQL to `done`.
4. Save the last processed line number.
5. Save the total number of matched lines.
6. Delete the processed file from the local Reader directory.

After successful completion, the Reader can call the Agent API again to request a new file.

**Important:**

The original file stored in MinIO must not be deleted after normal processing.

MinIO file deletion is exclusively managed by the Log Rotation mechanism.

---

## 11. File Processing Status API

The Reader must expose an API that allows the Agent to retrieve the processing status of previously submitted files.

The Agent sends one or more file identifiers, and the Reader retrieves their processing information from PostgreSQL.

### 11.1. API Authentication

The Agent calls `POST /status` with the Reader key in the `X-API-Key` header. Set `READER_API_KEY` in the Reader environment or local `.env`. The outbound `AGENT_API_KEY` is configured separately.

The endpoint runs in `-mode=agent` and listens on `READER_API_ADDR` (default `:8080`). Docker Compose runs the Reader in agent mode and publishes port 8080. An Agent outside Compose uses `http://<reader-host>:8080/status`. An Agent on the same Docker network uses `http://reader:8080/status`.

Requirements:

- The API Key must be loaded from the `.env` file.
- Every request must be authenticated.
- Invalid API Keys must result in an HTTP `401 Unauthorized` response.

### 11.2. API Input

The API must accept an array of file identifiers (`file_id`).

The Agent must send all requested file identifiers in a single JSON request.

Send 1 to 500 identifiers and keep the JSON request body at or below 64 KiB. Malformed requests or invalid identifiers return HTTP 400. Bodies larger than 64 KiB return HTTP 413.

#### Example Request

```json
{
  "file_ids": [
    "file_001",
    "file_002",
    "file_003"
  ]
}
```

### 11.3. API Response

The API must return a JSON response containing an array of file processing information.

#### Example Response

```json
{
  "files": [
    {
      "file_id": "file_001",
      "state": "reading",
      "lines_read": 120000,
      "lines_accepted": 118500
    },
    {
      "file_id": "file_002",
      "state": "done",
      "lines_read": 85000,
      "lines_accepted": 84000
    },
    {
      "file_id": "file_003",
      "state": "reading",
      "lines_read": 50000,
      "lines_accepted": 49500
    }
  ]
}
```

### 11.4. Response Fields

| Field | Description |
|---|---|
| `files` | Array containing known files in request order, with duplicate IDs included once. Unknown IDs are omitted. If none are known, the array is empty. |
| `file_id` | File identifier. |
| `state` | File processing status: `reading` or `done`. |
| `lines_read` | Number of lines read from the file. |
| `lines_accepted` | Number of valid and accepted lines. |

### 11.5. API Workflow

1. The Agent sends an array of `file_id` values.
2. The Reader validates the API Key.
3. The Reader retrieves the processing status of each requested file from PostgreSQL.
4. The Reader constructs the response.
5. The Reader returns all requested file information in a JSON array under the `files` key.

Each file must be represented as an independent JSON object.

---

## 12. Data Lifecycle Management and Log Rotation

File processing information must not remain permanently in the database.

The Reader must implement an automated Log Rotation mechanism to manage the lifecycle of stored information and MinIO files.

### 12.1. Data Retention Period

File records and their processing information must be retained for **one calendar month**.

Eligibility begins at the registration timestamp plus one calendar month in
UTC. If that day does not exist in the target month, use its last day at the
same UTC time.

Once an expired file is terminal:

- The corresponding database record must be deleted.
- The associated file must be eligible for deletion from MinIO.

### 12.2. MinIO File Deletion

Log Rotation is responsible for deleting the original files stored in MinIO after the retention period expires.

The process has two main responsibilities:

1. Delete expired file records from the Reader PostgreSQL database.
2. Delete the corresponding expired files from MinIO.

### 12.3. Log Rotation Execution

Log Rotation runs automatically in agent mode using
`LOG_ROTATION_INTERVAL` (a positive Go duration, default `24h`). Reader
attempts up to 100 candidates per run and retries failures on later runs.
The configured MinIO credentials need permission to delete objects in the
Reader bucket. Object keys identify individual source files and must be
unique to each registration. Use an unversioned bucket for physical
deletion. On a versioned bucket, removing a key can leave older versions
in storage.

Its execution interval must be configurable through the `.env` file.

Additional requirements:

- Only files in the terminal `done` state are eligible. Active, resumable,
  and unrecognized states are protected.
- Database and MinIO deletion operations must be performed in a controlled manner.
- If MinIO deletion fails, the process must support retry attempts.
- MinIO file deletion must never result in the removal of information associated with files that are still being processed.
- The database record is removed only after MinIO confirms deletion or absence.

---

## 13. Environment Configuration

All configuration values related to the Agent, PostgreSQL, MinIO, RabbitMQ, and file processing must be configurable through environment variables.

The application must not require source code modifications when runtime configuration changes.

### 13.1. Required Configuration Categories

| Category | Configuration |
|---|---|
| Agent | Agent API server address and port. |
| Agent Authentication | Agent API Key. |
| Reader Authentication | Reader status API Key. |
| Agent Communication | File retrieval polling interval. |
| Batch Processing | Number of lines per batch. |
| PostgreSQL | Database connection settings. |
| MinIO | MinIO connection settings. |
| RabbitMQ | RabbitMQ connection settings. |
| RabbitMQ Topics | Full record topic name. |
| RabbitMQ Topics | Unique URL topic name. |
| RabbitMQ Priority | Priority queue configuration. |
| Log Rotation | Log Rotation execution interval. |
| Data Retention | One calendar month from registration in UTC. |

### 13.2. Example Environment Variables

The following is an illustrative configuration structure. Exact variable names and values must be defined during implementation.

```env
# Agent Configuration
AGENT_HOST=
AGENT_PORT=
AGENT_API_KEY=
AGENT_POLLING_INTERVAL=

# Reader API Configuration
READER_API_KEY=
READER_API_ADDR=:8080

# PostgreSQL Configuration
DB_HOST=
DB_PORT=
DB_NAME=
DB_USER=
DB_PASSWORD=

# MinIO Configuration
MINIO_ENDPOINT=
MINIO_PORT=
MINIO_ACCESS_KEY=
MINIO_SECRET_KEY=
MINIO_BUCKET=

# RabbitMQ Configuration
RABBITMQ_HOST=
RABBITMQ_PORT=
RABBITMQ_USER=
RABBITMQ_PASSWORD=
RABBITMQ_VHOST=

# RabbitMQ Topics
RABBITMQ_FULL_RECORD_TOPIC=
RABBITMQ_UNIQUE_URL_TOPIC=

# RabbitMQ Priority
RABBITMQ_MAX_PRIORITY=

# Batch Processing
BATCH_SIZE=

# Log Rotation
LOG_ROTATION_INTERVAL=24h
```

**Note:** The environment variable names above are examples and do not represent finalized implementation names.

---

## 14. Overall Processing Workflow

The complete Reader workflow is described below:

1. The Reader application starts.
2. The Reader checks PostgreSQL for an existing file with the `reading` status.
3. If an active file exists, the Reader resumes processing from the last saved line.
4. If no active file exists, the Reader calls the Agent API to request a new file.
5. If a new file is available, the Reader receives its MinIO `file_id` and `priority`.
6. The Reader saves the file information in PostgreSQL.
7. The Reader downloads the file from MinIO.
8. The Reader inspects the first 5,000 lines to identify the appropriate Regex.
9. If no Regex matches, the Reader updates the file status to `done` and deletes the local file.
10. If a Regex is selected, the Reader starts batch processing.
11. The Reader extracts and validates records from each batch.
12. The Reader normalizes extracted URLs.
13. The Reader publishes complete records as independent messages to the first RabbitMQ topic.
14. The Reader publishes unique URLs with their corresponding priorities as independent messages to the second RabbitMQ topic.
15. After all batches have been processed, the Reader updates the file status to `done`.
16. The Reader saves the total number of lines read and accepted in PostgreSQL.
17. The Reader deletes the processed file from its local directory.
18. The Reader requests another file from the Agent.
19. The Agent can retrieve file processing statuses through the Reader Status API.
20. Log Rotation automatically deletes expired database records and MinIO files once their one-calendar-month retention ends, subject to the processing-state rules.

---

## 15. Architectural Requirements and Constraints

The Reader implementation must satisfy the following requirements:

| Requirement | Description |
|---|---|
| Single File Processing | Only one file can be processed at a time. |
| Persistent Processing State | Processing progress must be stored in PostgreSQL. |
| Processing Recovery | Interrupted processing must resume from the last saved line. |
| Memory Efficiency | Large files must be processed using configurable batches. |
| Regex-Based Extraction | File structure identification and data extraction must rely on predefined Regex patterns. |
| Data Validation | All extracted records must pass structural validation before publishing. |
| URL Normalization | URLs must be normalized before hashing and publishing. |
| Record Hashing | Every valid record must have a deterministic hash generated from its normalized URL, username, and password. |
| Independent Messages | Every record and unique URL must be published as an independent RabbitMQ message. |
| Priority Management | URL messages must inherit the priority of their source files. |
| API Authentication | The Reader Status API must require a valid API Key. |
| Automated Cleanup | Expired database records and MinIO files must be removed through Log Rotation. |
| Automated Testing | Core business logic, integrations, and critical processing workflows must be covered by automated tests. |
| Configurable Runtime | All external service connections and processing parameters must be configurable through `.env`. |

---

## 16. Final Architecture Summary

The Reader module provides a controlled and recoverable pipeline for processing large text files.

Its primary responsibilities include:

- Receiving file metadata and priorities from the Agent.
- Downloading files from MinIO.
- Identifying the appropriate Regex for each file.
- Processing large files using configurable batches.
- Extracting and validating URL, username, and password information.
- Generating record hashes and URL metadata.
- Publishing complete records and unique URLs to separate RabbitMQ topics.
- Preserving processing progress in PostgreSQL.
- Recovering interrupted processing after application restarts.
- Providing file processing status information to the Agent.
- Automatically managing database and MinIO file retention through Log Rotation.

The architecture must guarantee that:

1. Only one file is processed at any given time.
2. Large files are processed with controlled memory consumption.
3. Interrupted processing can resume from the last persisted position.
4. Previously processed lines are not unnecessarily processed again.
5. Extracted records are validated before being published.
6. RabbitMQ messages follow the defined data structures.
7. URL messages preserve the priority of their source files.
8. Processing status is accessible to the Agent through an authenticated API.
9. Expired files and their associated database records are automatically managed.

**The overall objective is to build a reliable, scalable, and fault-tolerant file processing module that efficiently handles large text files while maintaining data integrity, processing continuity, and message delivery consistency.**


---

## 17. Testing Requirements

The Reader module must include automated tests to verify the correctness and reliability of its components and processing workflows.

### 17.1. Unit Tests

Unit tests must cover the individual components and business logic, including:

- Regex matching and selection, including active and inactive Regex patterns.
- Data extraction, validation, and URL normalization.
- Record hash generation.
- Domain, subdomain, suffix, and port extraction.
- Batch processing and line-count calculations.
- Duplicate URL detection.
- File priority handling.
- Processing state and progress calculations.
- Log Rotation eligibility and retention-period calculations.

### 17.2. Integration Tests

Integration tests must verify interactions between the Reader and its dependencies, including:

- PostgreSQL operations, migrations, and predefined Regex insertion.
- Saving and retrieving file processing states and progress.
- MinIO file download and local file handling.
- RabbitMQ message publishing for full records and unique URLs.
- Reader Status API authentication, request handling, and response structure.
- Agent API communication and handling of available or unavailable files.

### 17.3. Workflow and Recovery Tests

Tests must verify the main processing workflows and important failure scenarios, including:

- Processing a file in configurable batches.
- Ensuring that only one file is processed at a time.
- Resuming an interrupted file from the last persisted line.
- Reusing the Regex selected before an interruption.
- Ensuring that completed lines are not unnecessarily processed again.
- Handling files for which no Regex matches.
- Updating the file status and processing counters after completion.
- Ensuring that local files are deleted only at the appropriate processing stage.
- Ensuring that files with the `reading` status are not deleted by Log Rotation.
- Retrying MinIO deletion when a Log Rotation deletion attempt fails.

### 17.4. Testability Requirements

- Tests must be repeatable and must not depend on production data or production credentials.
- External dependencies should be isolated or replaced with suitable test doubles where appropriate.
- Integration tests that require PostgreSQL, RabbitMQ, or MinIO must use dedicated test instances or isolated test resources.
- Tests must verify both successful operations and relevant error-handling scenarios.
- Test configuration and credentials must be separate from the application's production configuration.
- The test suite must be runnable through documented Go test commands.

**Important:** The project's Docker Compose configuration must continue to start only the Reader and PostgreSQL containers. RabbitMQ and MinIO used by integration tests must be provided through separately managed test infrastructure or suitable test doubles; they must not be added to the project's standard Docker Compose services.
