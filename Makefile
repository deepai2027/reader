.PHONY: build vet test run fmt

build:
	go build -o bin/reader ./cmd/reader

vet:
	go vet ./...

test:
	go test ./...

fmt:
	gofmt -l .

run: build
	./bin/reader
