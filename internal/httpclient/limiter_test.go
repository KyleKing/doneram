package httpclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostLimiterBoundsConcurrency(t *testing.T) {
	var live, peak int64
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&live, 1)
		mu.Lock()
		if n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt64(&live, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.HostConcurrency = 2
	client := New(cfg)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()

	if peak > 2 {
		t.Errorf("peak concurrency = %d, want at most 2", peak)
	}
}

func TestHostLimiterFailsFastOnSpentQuota(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := New(DefaultConfig())
	for i := 0; i < 3; i++ {
		resp, err := client.Get(server.URL)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("err = nil, want a rate limit error")
		}
		var limit *RateLimitError
		if !errors.As(err, &limit) {
			t.Fatalf("err = %v, want a RateLimitError", err)
		}
	}

	if requests != 1 {
		t.Errorf("server saw %d requests, want 1 before failing fast", requests)
	}
}
