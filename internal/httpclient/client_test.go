package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryTransport_Success(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(DefaultConfig())
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetryTransport_RetryOn500(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	client := New(cfg)

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryTransport_RateLimitWithRetryAfter(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	client := New(cfg)

	start := time.Now()
	resp, err := client.Get(server.URL)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}

	if duration < 1*time.Second {
		t.Errorf("expected retry after at least 1s, got %v", duration)
	}
}

func TestRetryTransport_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(DefaultConfig())
	_, err := client.Get(server.URL)

	var notFoundErr *NotFoundError
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !isNotFoundError(err, &notFoundErr) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestRetryTransport_MaxRetriesExceeded(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.MaxRetries = 2
	cfg.BaseDelay = 10 * time.Millisecond
	client := New(cfg)

	_, err := client.Get(server.URL)

	var retryErr *RetryableError
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !isRetryableError(err, &retryErr) {
		t.Errorf("expected RetryableError, got %T: %v", err, err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", attempts)
	}
}

func TestRetryTransport_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := New(DefaultConfig())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)

	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
}

func isNotFoundError(err error, target **NotFoundError) bool {
	return errors.As(err, target)
}

func isRetryableError(err error, target **RetryableError) bool {
	return errors.As(err, target)
}
