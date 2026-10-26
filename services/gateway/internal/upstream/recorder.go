package upstream

import (
	"context"
	"log/slog"
	"time"
)

// CallRecord represents the telemetry data recorded for a single tool call through the gateway.
type CallRecord struct {
	User       string        `json:"user"`
	ServerSlug string        `json:"server_slug"`
	Tool       string        `json:"tool"`
	StartedAt  time.Time     `json:"started_at"`
	Duration   time.Duration `json:"duration"`
	Outcome    string        `json:"outcome"` // "success", "error", "timeout"
	Error      string        `json:"error,omitempty"`
}

// CallRecorder defines the interface for recording tool call telemetry.
// Chunk 3 uses a slog implementation. Chunk 4 plugs in the database audit log.
type CallRecorder interface {
	Record(ctx context.Context, record CallRecord)
}

// SlogCallRecorder logs tool calls using structured slog.
type SlogCallRecorder struct {
	logger *slog.Logger
}

// NewSlogCallRecorder creates a new SlogCallRecorder.
func NewSlogCallRecorder(logger *slog.Logger) *SlogCallRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogCallRecorder{logger: logger}
}

// Record logs the tool call completion.
func (r *SlogCallRecorder) Record(_ context.Context, rec CallRecord) {
	attrs := []any{
		slog.String("user", rec.User),
		slog.String("server", rec.ServerSlug),
		slog.String("tool", rec.Tool),
		slog.Int64("duration_ms", rec.Duration.Milliseconds()),
		slog.String("outcome", rec.Outcome),
	}
	if rec.Error != "" {
		attrs = append(attrs, slog.String("error", rec.Error))
	}

	if rec.Outcome == "success" {
		r.logger.Info("tool call completed", attrs...)
	} else {
		r.logger.Warn("tool call completed with failure", attrs...)
	}
}
