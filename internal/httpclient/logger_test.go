package httpclient

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestContextWithLoggerRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx := ContextWithLogger(context.Background(), logger)
	if got := LoggerFromContext(ctx); got != logger {
		t.Error("LoggerFromContext did not return the logger stored in the context")
	}
}

func TestLoggerFromContextFallsBackToDefault(t *testing.T) {
	if got := LoggerFromContext(context.Background()); got != slog.Default() {
		t.Error("LoggerFromContext should fall back to slog.Default when no logger is stored")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("parseRetryAfter(\"\") = %v, want 0", got)
	}
	if got := parseRetryAfter("30"); got != 30*time.Second {
		t.Errorf("parseRetryAfter(\"30\") = %v, want 30s", got)
	}
	if got := parseRetryAfter("not a delay"); got != 0 {
		t.Errorf("parseRetryAfter(\"not a delay\") = %v, want 0", got)
	}

	future := time.Now().Add(time.Hour).UTC().Format(time.RFC1123)
	if got := parseRetryAfter(future); got <= 0 {
		t.Errorf("parseRetryAfter(%q) = %v, want a positive delay", future, got)
	}
}
