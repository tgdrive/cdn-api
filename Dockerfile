# syntax=docker/dockerfile:1

FROM golang:alpine AS builder

RUN apk add --no-cache git ca-certificates

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum* ./

RUN go mod download

COPY . .

RUN go build -ldflags="-s -w" -o cdn-api .
    
FROM scratch

COPY --from=builder /app/cdn-api .

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=builder /etc/mime.types /etc/mime.types

ENTRYPOINT ["/cdn-api"]