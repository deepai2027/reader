.PHONY: build vet test test-integration run fmt

# -buildvcs=false: avoids "error obtaining VCS status" when git can't read
# the repo's status cleanly (e.g. right after unzipping, or a git
# "dubious ownership" check failing). Phase 0 doesn't need VCS stamping
# in the binary; drop this flag later if you want it back.
build:
	go build -buildvcs=false -o bin/reader ./cmd/reader

vet:
	go vet ./...

test:
	go test ./...

# Requires Docker (testcontainers-go spins up ephemeral Postgres,
# RabbitMQ, and MinIO). See specs/2026-11-10-mvp/validation.md.
test-integration:
	go test -tags=integration ./test/integration/...

fmt:
	gofmt -l .

run: build
	./bin/reader
