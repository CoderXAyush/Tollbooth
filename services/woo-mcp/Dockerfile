# ── Stage 1: Build ──────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /woo-mcp ./cmd/main.go

# ── Stage 2: Runtime ────────────────────────────────────────
FROM alpine:3.21

RUN addgroup -S appgroup && adduser -S appuser -G appgroup
RUN apk add --no-cache ca-certificates

COPY --from=builder /woo-mcp /usr/local/bin/woo-mcp

USER appuser

EXPOSE 8081
ENTRYPOINT ["woo-mcp"]
