// Package osv queries https://osv.dev for known vulnerabilities affecting a
// resolved package version, and computes the minimum version that clears
// every advisory found.
package osv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/pkg/version"
)

// Query is one package+ecosystem+version to check against OSV.
type Query struct {
	Package   string
	Ecosystem string
	Version   string
}

// Advisory is one OSV record affecting a queried package, trimmed to what a
// report needs: identity, severity, a link, and the versions that fix it.
type Advisory struct {
	ID       string
	Summary  string
	Severity string
	URL      string
	Fixed    []string
}

// MinimumFix returns the smallest fixed version across advisories that is
// greater than current, or "" when no advisory names a fix above current.
func MinimumFix(advisories []Advisory, current string) string {
	var min string
	currentVer := version.Parse(current)
	for _, a := range advisories {
		for _, fixed := range a.Fixed {
			if version.Compare(version.Parse(fixed), currentVer) <= 0 {
				continue
			}
			if min == "" || version.Compare(version.Parse(fixed), version.Parse(min)) < 0 {
				min = fixed
			}
		}
	}
	return min
}

type Client struct {
	client  *http.Client
	baseURL string
}

func New(client *http.Client) *Client {
	return NewWithBaseURL(client, "https://api.osv.dev")
}

func NewWithBaseURL(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL}
}

type batchQueryPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type batchQuery struct {
	Package batchQueryPackage `json:"package"`
	Version string            `json:"version"`
}

type batchRequest struct {
	Queries []batchQuery `json:"queries"`
}

type batchVuln struct {
	ID string `json:"id"`
}

type batchResult struct {
	Vulns []batchVuln `json:"vulns"`
}

type batchResponse struct {
	Results []batchResult `json:"results"`
}

// Query batches every query into one request to /v1/querybatch, then fetches
// the full record for each distinct advisory ID found, and returns the
// advisories affecting each query in the same order as queries.
func (c *Client) Query(ctx context.Context, queries []Query) ([][]Advisory, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	ids, err := c.batchQuery(ctx, queries)
	if err != nil {
		return nil, err
	}

	uniqueIDs := make(map[string]struct{})
	for _, perQuery := range ids {
		for _, id := range perQuery {
			uniqueIDs[id] = struct{}{}
		}
	}

	logger := httpclient.LoggerFromContext(ctx)
	records := make(map[string]vulnRecord, len(uniqueIDs))
	for id := range uniqueIDs {
		rec, err := c.fetchVuln(ctx, id)
		if err != nil {
			logger.Warn("failed to fetch OSV advisory, skipping", "id", id, "error", err)
			continue
		}
		records[id] = rec
	}

	out := make([][]Advisory, len(queries))
	for i, q := range queries {
		for _, id := range ids[i] {
			rec, ok := records[id]
			if !ok {
				continue
			}
			out[i] = append(out[i], rec.toAdvisory(q.Ecosystem))
		}
	}
	return out, nil
}

func (c *Client) batchQuery(ctx context.Context, queries []Query) ([][]string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("querying OSV", "resolver", "osv", "count", len(queries))

	req := batchRequest{Queries: make([]batchQuery, len(queries))}
	for i, q := range queries {
		req.Queries[i] = batchQuery{
			Package: batchQueryPackage{Name: q.Package, Ecosystem: q.Ecosystem},
			Version: q.Version,
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling OSV batch request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/querybatch", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("creating OSV request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		logger.Warn("failed to query OSV", "error", err)
		return nil, fmt.Errorf("querying OSV: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSV unavailable (status %d): retry later", resp.StatusCode)
	}

	var out batchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding OSV batch response: %w", err)
	}

	ids := make([][]string, len(queries))
	for i, result := range out.Results {
		if i >= len(queries) {
			break
		}
		for _, v := range result.Vulns {
			ids[i] = append(ids[i], v.ID)
		}
	}
	return ids, nil
}

type vulnRange struct {
	Type   string `json:"type"`
	Events []struct {
		Introduced string `json:"introduced"`
		Fixed      string `json:"fixed"`
	} `json:"events"`
}

type vulnAffected struct {
	Package struct {
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Ranges []vulnRange `json:"ranges"`
}

type vulnSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type vulnRecord struct {
	ID               string         `json:"id"`
	Summary          string         `json:"summary"`
	Details          string         `json:"details"`
	Severity         []vulnSeverity `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	References []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"references"`
	Affected []vulnAffected `json:"affected"`
}

// toAdvisory trims a full OSV record to the ecosystem a query asked about,
// since one advisory often covers several distro releases with different
// fixed versions each.
func (r vulnRecord) toAdvisory(ecosystem string) Advisory {
	a := Advisory{
		ID:       r.ID,
		Summary:  r.Summary,
		Severity: r.severity(),
		URL:      r.url(),
	}
	for _, affected := range r.Affected {
		if affected.Package.Ecosystem != ecosystem {
			continue
		}
		for _, rng := range affected.Ranges {
			if rng.Type == "GIT" {
				continue
			}
			for _, event := range rng.Events {
				if event.Fixed != "" {
					a.Fixed = append(a.Fixed, event.Fixed)
				}
			}
		}
	}
	return a
}

func (r vulnRecord) severity() string {
	if r.DatabaseSpecific.Severity != "" {
		return r.DatabaseSpecific.Severity
	}
	if len(r.Severity) > 0 {
		return r.Severity[0].Score
	}
	return ""
}

func (r vulnRecord) url() string {
	for _, ref := range r.References {
		if ref.Type == "ADVISORY" {
			return ref.URL
		}
	}
	for _, ref := range r.References {
		if ref.Type == "WEB" {
			return ref.URL
		}
	}
	return "https://osv.dev/vulnerability/" + r.ID
}

func (c *Client) fetchVuln(ctx context.Context, id string) (vulnRecord, error) {
	var rec vulnRecord

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/vulns/"+id, nil)
	if err != nil {
		return rec, fmt.Errorf("creating request for %s: %w", id, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return rec, fmt.Errorf("fetching advisory %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return rec, fmt.Errorf("OSV unavailable (status %d) for advisory %s: retry later", resp.StatusCode, id)
	}

	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return rec, fmt.Errorf("decoding advisory %s: %w", id, err)
	}
	return rec, nil
}
