.PHONY: build vet test run fmt

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

fmt:
	gofmt -l .

run: build
	./bin/reader
