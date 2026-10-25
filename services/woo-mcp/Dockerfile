# ── Stage 1: Build ──────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /woo-mcp ./cmd/main.go

# ── Stage 2: Runtime (Distroless nonroot) ─────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /woo-mcp /usr/local/bin/woo-mcp

USER nonroot:nonroot

EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/woo-mcp"]
