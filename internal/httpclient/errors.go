package httpclient

import (
	"fmt"
	"time"
)

type RetryableError struct {
	Err      error
	Attempts int
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("request failed after %d attempts: %v", e.Attempts, e.Err)
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (status %d): retry after %v", e.StatusCode, e.RetryAfter)
}

type NotFoundError struct {
	Resource string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("resource not found: %s", e.Resource)
}
