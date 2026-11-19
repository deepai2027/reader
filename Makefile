.PHONY: build vet test test-integration run fmt

build:
	go build -buildvcs=false -o bin/reader ./cmd/reader

vet:
	go vet ./...

test:
	go test ./...

test-integration:
	go test -tags=integration ./test/integration/...

fmt:
	gofmt -l .

run: build
	./bin/reader

run-agent: build
	./bin/reader -mode=agent
