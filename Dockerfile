# Multi-stage build for Fairway
FROM golang:1.23-alpine AS builder

WORKDIR /build

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /build/fairway ./cmd/fairway

# Minimal production runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN addgroup -S fairway && adduser -S fairway -G fairway

COPY --from=builder /build/fairway /app/fairway
COPY corridors.yaml /app/corridors.yaml

USER fairway

EXPOSE 8080

ENTRYPOINT ["/app/fairway"]
