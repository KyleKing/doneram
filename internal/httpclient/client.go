package httpclient

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

type Config struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Timeout    time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
		Timeout:    30 * time.Second,
	}
}

type retryTransport struct {
	transport  http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	logger := LoggerFromContext(req.Context())
	var lastErr error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			delay := t.calculateDelay(attempt)
			logger.Debug("retrying request",
				"url", req.URL.String(),
				"attempt", attempt,
				"delay", delay)

			select {
			case <-time.After(delay):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		resp, err := t.transport.RoundTrip(req)
		if err != nil {
			lastErr = err
			if !isRetryable(err) {
				return nil, err
			}
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, &NotFoundError{Resource: req.URL.String()}
		}

		if !shouldRetry(resp.StatusCode) {
			return resp, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			logger.Warn("rate limit hit",
				"url", req.URL.String(),
				"retry_after", retryAfter)
			resp.Body.Close()
			lastErr = &RateLimitError{
				StatusCode: resp.StatusCode,
				RetryAfter: retryAfter,
			}

			if retryAfter > 0 && retryAfter <= t.maxDelay {
				select {
				case <-time.After(retryAfter):
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			continue
		}

		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return nil, &RetryableError{
		Err:      lastErr,
		Attempts: t.maxRetries + 1,
	}
}

func (t *retryTransport) calculateDelay(attempt int) time.Duration {
	delay := t.baseDelay * time.Duration(1<<uint(attempt-1))
	if delay > t.maxDelay {
		delay = t.maxDelay
	}

	jitter := time.Duration(rand.Int63n(int64(delay) / 4))
	if rand.Intn(2) == 0 {
		delay += jitter
	} else {
		delay -= jitter
	}

	return delay
}

func isRetryable(err error) bool {
	return true
}

func shouldRetry(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}

	if t, err := time.Parse(time.RFC1123, header); err == nil {
		return time.Until(t)
	}

	return 0
}

func New(cfg Config) *http.Client {
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &retryTransport{
			transport:  http.DefaultTransport,
			maxRetries: cfg.MaxRetries,
			baseDelay:  cfg.BaseDelay,
			maxDelay:   cfg.MaxDelay,
		},
	}
}
