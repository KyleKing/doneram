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
	// Ended is set when endoflife.date marks the cycle ended without a
	// date (`"eol": true`).
	Ended bool
}

// Past reports whether the cycle has reached end of life by now.
func (s Status) Past(now time.Time) bool {
	return s.Ended || (!s.Date.IsZero() && s.DaysLeft(now) < 0)
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
	Cycle    string          `json:"cycle"`
	Codename string          `json:"codename"`
	EOL      json.RawMessage `json:"eol"`
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
	best := longestCycle(cycles, version, ".")
	if best == nil {
		// A distro release is often named by codename ("bookworm").
		for i := range cycles {
			if cycles[i].Codename != "" && strings.EqualFold(cycles[i].Codename, version) {
				best = &cycles[i]
			}
		}
	}
	if best == nil {
		return Status{}, fmt.Errorf("endoflife.date lists no %s cycle covering %s", product, version)
	}
	return best.status(product)
}

// LookupID finds the cycle an identifier belongs to where cycles are names
// rather than version numbers: a model ID ("claude-opus-4-8-20260101")
// lands in its longest cycle equal to it or prefixing it at a dash, and a
// runner label ("ubuntu-22.04") in the cycle of the same name.
func (c *Client) LookupID(ctx context.Context, product, id string) (Status, error) {
	cycles, err := c.fetch(ctx, product)
	if err != nil {
		return Status{}, err
	}
	best := longestCycle(cycles, id, "-")
	if best == nil {
		return Status{}, fmt.Errorf("endoflife.date lists no %s cycle covering %s", product, id)
	}
	return best.status(product)
}

// longestCycle returns the longest cycle equal to v or prefixing it at sep.
func longestCycle(cycles []cycle, v, sep string) *cycle {
	var best *cycle
	for i := range cycles {
		name := cycles[i].Cycle
		if v != name && !strings.HasPrefix(v, name+sep) {
			continue
		}
		if best == nil || len(name) > len(best.Cycle) {
			best = &cycles[i]
		}
	}
	return best
}

func (best *cycle) status(product string) (Status, error) {
	status := Status{Product: product, Cycle: best.Cycle}
	var ended bool
	if json.Unmarshal(best.EOL, &ended) == nil {
		status.Ended = ended
		return status, nil
	}
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
