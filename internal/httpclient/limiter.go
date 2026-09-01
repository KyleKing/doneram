package httpclient

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultHostConcurrency bounds in-flight requests to a single host.
const DefaultHostConcurrency = 4

// hostLimiter paces requests per host and fails fast once a host reports its
// quota spent, since a GitHub window resets on the hour and no backoff inside
// one run outlasts it.
type hostLimiter struct {
	transport http.RoundTripper
	limit     int

	mu        sync.Mutex
	slots     map[string]chan struct{}
	exhausted map[string]time.Time
}

func newHostLimiter(transport http.RoundTripper, limit int) *hostLimiter {
	if limit < 1 {
		limit = DefaultHostConcurrency
	}
	return &hostLimiter{
		transport: transport,
		limit:     limit,
		slots:     make(map[string]chan struct{}),
		exhausted: make(map[string]time.Time),
	}
}

func (l *hostLimiter) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host

	if err := l.checkExhausted(host); err != nil {
		return nil, err
	}

	slot := l.slot(host)
	select {
	case slot <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	defer func() { <-slot }()

	resp, err := l.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	reset, spent := quotaSpent(resp)
	if !spent {
		return resp, nil
	}

	l.mu.Lock()
	l.exhausted[host] = reset
	l.mu.Unlock()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		return nil, quotaError(host, resp.StatusCode, time.Until(reset))
	}
	return resp, nil
}

func (l *hostLimiter) checkExhausted(host string) error {
	l.mu.Lock()
	reset, ok := l.exhausted[host]
	if ok && !time.Now().Before(reset) {
		delete(l.exhausted, host)
		ok = false
	}
	l.mu.Unlock()

	if !ok {
		return nil
	}
	return quotaError(host, http.StatusForbidden, time.Until(reset))
}

func (l *hostLimiter) slot(host string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot, ok := l.slots[host]
	if !ok {
		slot = make(chan struct{}, l.limit)
		l.slots[host] = slot
	}
	return slot
}

func quotaError(host string, status int, retryAfter time.Duration) error {
	err := &RateLimitError{StatusCode: status, RetryAfter: retryAfter}
	if strings.HasSuffix(host, "github.com") {
		return fmt.Errorf("%s: %w; set GITHUB_TOKEN to lift the unauthenticated limit of 60 requests an hour", host, err)
	}
	return fmt.Errorf("%s: %w", host, err)
}

func quotaSpent(resp *http.Response) (time.Time, bool) {
	remaining, err := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	if err != nil || remaining > 0 {
		return time.Time{}, false
	}
	epoch, err := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	if err != nil {
		return time.Now().Add(time.Hour), true
	}
	return time.Unix(epoch, 0), true
}
