# Reader — Tech Stack

This document fixes the concrete technical choices behind the stakeholder
requirements in the README. Where the README was prescriptive (Go,
PostgreSQL, RabbitMQ, MinIO, Docker Compose), those choices are carried
over unchanged. Where it left room, this document makes a decision and
states the reasoning, so implementation doesn't stall on bikeshedding.

**Guiding bias: minimal dependencies.** Per stakeholder decision, HTTP is
built on the standard library only — this same bias (prefer stdlib,
add a dependency only when stdlib genuinely can't do the job safely)
is applied consistently below.

## Language & Runtime

| Choice | Value |
|---|---|
| Language | Go |
| Minimum version | Go 1.22+ |
| Rationale | Go 1.22 brings pattern-based routing (method + path wildcards) to `net/http.ServeMux`, which removes the usual reason teams reach for a router framework. |

## HTTP Layer — Standard Library Only

Both HTTP responsibilities are built on `net/http` from the standard
library, with no third-party router or client framework:

- **Reader Status API (server):** `http.ServeMux` (Go 1.22+ pattern
  routing, e.g. `POST /status`) with hand-written middleware for API-key
  authentication and structured request logging. No Gin/Echo/Chi.
- **Agent API client:** `net/http.Client` with an explicit `Timeout` and a
  small internal wrapper for the polling loop, 409 handling, and
  API-key header injection.

**Trade-off accepted:** slightly more boilerplate for middleware chaining
and JSON binding than a framework would give. Acceptable given the API
surface is small (one outbound polling client, one inbound status
endpoint).

## Database

| Concern | Choice | Rationale |
|---|---|---|
| Database | PostgreSQL | Fixed by README. |
| Driver | `jackc/pgx` (`pgx/v5`, used via `database/sql` with `pgx/stdlib`) | Actively maintained, better performance and type handling than `lib/pq` (which is in maintenance mode). Using it through `database/sql` keeps the codebase idiomatic and testable with standard mocking patterns. |
| Migrations | `golang-migrate/migrate` | Industry-standard, CLI + library usage, plain SQL migration files — keeps predefined Regex seeding as a reviewable SQL migration rather than app-boot logic. |
| Query style | Plain SQL via `database/sql`, no ORM | The schema is small (2 tables) and query patterns are simple; an ORM would add indirection without real benefit. Hand-written SQL also keeps intent (especially around `FOR UPDATE` locking for single-file-processing guarantees) explicit and auditable. |

## Message Broker

| Concern | Choice | Rationale |
|---|---|---|
| Broker | RabbitMQ | Fixed by README (externally managed, not started by this project's Compose file). |
| Client library | `rabbitmq/amqp091-go` | The official, maintained successor to the archived `streadway/amqp`. Supports `x-max-priority` queue arguments needed for priority queues. |
| Publishing pattern | Confirm-mode publishing, one message per record/URL, batched *calls* (not batched *payloads*) per processing batch | Matches the "independent messages, never combined" requirement while still gaining throughput from publishing a batch's worth of messages together before waiting on confirms. |

## Object Storage

| Concern | Choice | Rationale |
|---|---|---|
| Storage | MinIO | Fixed by README (externally managed). |
| Client library | `minio/minio-go` (v7) | Official Go SDK, S3-compatible, handles download streaming without loading full objects into memory — required for large-file support. |

## Regex Engine — Important Open Risk

| Concern | Choice | Rationale |
|---|---|---|
| Engine | Standard library `regexp` (RE2 syntax) | Zero dependency, linear-time matching (no catastrophic backtracking risk on adversarial/huge input — a real concern when scanning untrusted file content line-by-line at scale). |

**Flag for stakeholder confirmation:** Go's `regexp` package is RE2-based
and does **not** support backreferences or lookahead/lookbehind
assertions. Credential-list style text formats sometimes rely on these
features for disambiguation. If the predefined Regex patterns (seeded in
migrations) need backreferences or lookaround, the fallback is
`dlclark/regexp2` (a single, well-maintained dependency with .NET-style
regex semantics, at the cost of RE2's linear-time guarantee). **This
roadmap assumes stdlib `regexp` is sufficient until the actual Regex
patterns are provided** — revisit before Phase 3 (Regex selection).

## URL Normalization & Domain Parsing

| Concern | Choice | Rationale |
|---|---|---|
| URL parsing | Standard library `net/url` | Sufficient for parsing, normalizing scheme/host/path/port. |
| Domain/subdomain/suffix extraction | `golang.org/x/net/publicsuffix` | Correctly splits a host into subdomain/domain/suffix using the public suffix list (handles multi-part suffixes like `.co.uk` correctly) — not safely reimplementable by hand. This is an official Go sub-repository, maintained by the Go team, as close to "stdlib" as a dependency gets. |

## Hashing

| Concern | Choice | Rationale |
|---|---|---|
| Record hash | Standard library `crypto/sha256`, hex-encoded | Deterministic, dependency-free, matches `hash(url\|user\|password)` formula from the README. |

## Logging

| Concern | Choice | Rationale |
|---|---|---|
| Logging | Standard library `log/slog` | Structured logging (as required by the README) shipped in stdlib since Go 1.21 — no need for `zap`/`zerolog`. |

## Configuration

| Concern | Choice | Rationale |
|---|---|---|
| Loading `.env` in local/dev | `joho/godotenv` (dev/local only — not used in production containers, where real env vars are injected by Docker Compose / the orchestrator) | Tiny, single-purpose, widely used; avoids hand-rolling `.env` file parsing. |
| Reading config into typed struct | Hand-written internal `config` package using `os.Getenv` + explicit parsing/validation at startup (fail fast on missing/invalid required vars) | Keeps the "minimal dependencies" bias; the config surface (documented in README §13) is small and stable enough that a reflection-based library (`envconfig`, `caarlos0/env`) isn't worth the dependency. |

## Containerization

| Concern | Choice | Rationale |
|---|---|---|
| Containerization | Docker + Docker Compose | Fixed by README. |
| Compose scope | **Only** `reader` and `postgres` services | Explicit stakeholder constraint — RabbitMQ and MinIO are assumed to already be running in shared infrastructure and must never be started by this project. |
| Base image | Multi-stage build, `golang:1.2x` builder → `gcr.io/distroless/static` or `alpine` runtime | Small attack surface, small image, no need for a shell in production unless debugging requires it. |

## Testing

| Concern | Choice | Rationale |
|---|---|---|
| Unit tests | Standard library `testing`, table-driven tests | Fixed by README. No assertion library added by default — table-driven stdlib tests cover the required unit-test surface (regex selection, extraction, validation, hashing, domain parsing, batch math, retention eligibility) without a new dependency. |
| Integration tests | Standard library `testing` + build tag (e.g. `//go:build integration`) to separate from unit tests | Lets `go test ./...` stay fast by default; integration tests run explicitly via a documented `go test -tags=integration ./...` command, against dedicated test infrastructure (per README §17.4), never production credentials. |
| Test doubles | Hand-written interfaces + fakes for Agent API client, RabbitMQ publisher, MinIO client | Keeps unit tests free of network/infra dependency while still exercising real business logic (batching, recovery, retention). |
| Test-only dependency (optional, non-production) | `testcontainers-go`, for spinning up ephemeral Postgres/RabbitMQ/MinIO in integration tests, if dedicated test infra isn't otherwise provided | Does not affect the production binary or image; purely a `go.mod` `// test` dependency. To be confirmed once test infrastructure strategy is decided. |

## Project Layout (Standard Go Layout)

```text
reader/
├── cmd/
│   └── reader/              # main.go — composition root
├── internal/
│   ├── agent/                # Agent API client (polling, 409 handling)
│   ├── config/                # env loading & validation
│   ├── logrotation/           # retention + MinIO/DB cleanup
│   ├── minio/                # MinIO client wrapper
│   ├── processing/           # batch engine, recovery, orchestration
│   ├── rabbitmq/             # publisher, topic definitions
│   ├── regexselect/            # regex storage, selection logic
│   ├── statusapi/            # Reader Status API (server)
│   └── storage/              # PostgreSQL repositories (File Mgmt, Regex)
├── migrations/               # golang-migrate SQL files (incl. regex seed)
├── docker-compose.yml         # reader + postgres only
├── Dockerfile
├── .env.example
└── go.mod
```

## Summary Dependency List

| Dependency | Scope | Why it's there despite the minimal-deps bias |
|---|---|---|
| `jackc/pgx` | prod | No stdlib Postgres driver exists; this is the modern standard choice. |
| `golang-migrate/migrate` | prod/CLI | Versioned, reviewable schema + seed migrations. |
| `rabbitmq/amqp091-go` | prod | No stdlib AMQP client; official maintained library. |
| `minio/minio-go` | prod | No stdlib S3/MinIO client; official SDK, streaming downloads. |
| `golang.org/x/net/publicsuffix` | prod | Correct domain/suffix parsing is infeasible to hand-roll correctly; Go-team-maintained. |
| `joho/godotenv` | dev only | Convenience `.env` loader for local development only. |
| `testcontainers-go` | test only | Ephemeral infra for integration tests, pending test-infra decision. |

Everything else — HTTP, logging, hashing, URL parsing, regex matching,
config parsing, unit testing — is standard library.
