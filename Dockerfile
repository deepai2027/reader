# Build stage
FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -o /reader ./cmd/reader

## Production image
#FROM gcr.io/distroless/static AS production
#COPY --from=builder /reader /reader
#ENTRYPOINT ["/reader"]

# Debug image
FROM alpine:latest AS debug
RUN apk add --no-cache ca-certificates
COPY --from=builder /reader /reader
ENTRYPOINT ["/bin/sh"]
