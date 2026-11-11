// Package eol looks up end-of-life dates on https://endoflife.date for the
// release cycle a pinned version belongs to.
package eol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle of the cycle a pin belongs to. A zero Date means
// endoflife.date publishes no end-of-life date for that cycle yet.
type Status struct {
	Product string
	Cycle   string
	Date    time.Time
}

// DaysLeft counts whole days from now until Date, negative once past it.
func (s Status) DaysLeft(now time.Time) int {
	return int(s.Date.Sub(now.Truncate(24*time.Hour)).Hours() / 24)
}

type Client struct {
	client  *http.Client
	baseURL string

	mu     sync.Mutex
	cycles map[string][]cycle
}

func New(client *http.Client) *Client {
	return NewWithBaseURL(client, "https://endoflife.date")
}

func NewWithBaseURL(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL, cycles: map[string][]cycle{}}
}

type cycle struct {
	Cycle string          `json:"cycle"`
	EOL   json.RawMessage `json:"eol"`
}

// Lookup finds the cycle of product that version belongs to, the longest
// published cycle equal to version or prefixing it at a dot boundary, so
// 3.13.5 lands in 3.13 and 17.11 in 17. A version no cycle covers fails.
func (c *Client) Lookup(ctx context.Context, product, version string) (Status, error) {
	cycles, err := c.fetch(ctx, product)
	if err != nil {
		return Status{}, err
	}

	version = strings.TrimPrefix(version, "v")
	var best *cycle
	for i := range cycles {
		name := cycles[i].Cycle
		if version != name && !strings.HasPrefix(version, name+".") {
			continue
		}
		if best == nil || len(name) > len(best.Cycle) {
			best = &cycles[i]
		}
	}
	if best == nil {
		return Status{}, fmt.Errorf("endoflife.date lists no %s cycle covering %s", product, version)
	}

	status := Status{Product: product, Cycle: best.Cycle}
	var date string
	if json.Unmarshal(best.EOL, &date) == nil {
		parsed, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return Status{}, fmt.Errorf("parsing %s %s eol %q: %w", product, best.Cycle, date, err)
		}
		status.Date = parsed
	}
	return status, nil
}

func (c *Client) fetch(ctx context.Context, product string) ([]cycle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.cycles[product]; ok {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/%s.json", c.baseURL, product), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching endoflife.date %s: %w", product, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching endoflife.date %s: status %d", product, resp.StatusCode)
	}

	var cycles []cycle
	if err := json.NewDecoder(resp.Body).Decode(&cycles); err != nil {
		return nil, fmt.Errorf("decoding endoflife.date %s: %w", product, err)
	}
	c.cycles[product] = cycles
	return cycles, nil
}
