package httpclient

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRetryableErrorMessageAndUnwrap(t *testing.T) {
	cause := errors.New("connection reset")
	err := &RetryableError{Err: cause, Attempts: 3}

	if !strings.Contains(err.Error(), "3 attempts") {
		t.Errorf("Error() = %q, want the attempt count", err.Error())
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("Error() = %q, want the wrapped cause", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the wrapped cause")
	}
}

func TestRateLimitErrorMessage(t *testing.T) {
	err := &RateLimitError{StatusCode: 429, RetryAfter: 30 * time.Second}

	msg := err.Error()
	if !strings.Contains(msg, "429") || !strings.Contains(msg, "30s") {
		t.Errorf("Error() = %q, want the status code and retry delay", msg)
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	err := &NotFoundError{Resource: "alpine"}

	if !strings.Contains(err.Error(), "alpine") {
		t.Errorf("Error() = %q, want the resource name", err.Error())
	}
}

func TestErrorsAreAssignableToError(t *testing.T) {
	var _ error = &RetryableError{}
	var _ error = &RateLimitError{}
	var _ error = &NotFoundError{}
}
