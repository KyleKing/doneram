package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// YankChecker is implemented by a Resolver whose registry records that a
// published version was withdrawn: yanked on PyPI and crates.io, deprecated
// on npm. Resolve never offers such a version; Yanked reports whether the
// version already pinned has been withdrawn since, since that one is
// installed everywhere the pin is.
type YankChecker interface {
	Yanked(ctx context.Context, pkg, version string) (reason string, yanked bool, err error)
}

// docCache fetches each registry document once per run, so Resolve and
// Yanked asking about the same package cost one request.
type docCache struct {
	mu      sync.Mutex
	entries map[string]*docEntry
}

type docEntry struct {
	once sync.Once
	body []byte
	err  error
}

// get returns the body of url, fetched at most once. registry names the
// service in error messages, and header, when set, is applied to the
// request.
func (c *docCache) get(ctx context.Context, client *http.Client, url, registry, subject string, header http.Header) ([]byte, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*docEntry{}
	}
	e, ok := c.entries[url]
	if !ok {
		e = &docEntry{}
		c.entries[url] = e
	}
	c.mu.Unlock()

	e.once.Do(func() {
		e.body, e.err = fetchDoc(ctx, client, url, registry, subject, header)
	})
	return e.body, e.err
}

func fetchDoc(ctx context.Context, client *http.Client, url, registry, subject string, header http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s data: %w", registry, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s unavailable (status %d) for %s: retry later", registry, resp.StatusCode, subject)
	}
	return io.ReadAll(resp.Body)
}

func decodeDoc[T any](body []byte, registry string) (T, error) {
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decoding %s response: %w", registry, err)
	}
	return out, nil
}
