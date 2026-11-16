# Multi-stage build per specs/tech-stack.md.
FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -o /reader ./cmd/reader

FROM gcr.io/distroless/static
COPY --from=builder /reader /reader
ENTRYPOINT ["/reader"]
