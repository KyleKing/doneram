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
	"sync"

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
	ranges   []interval
}

// interval is one [introduced, fixed) window an advisory covers. An empty
// fixed means the advisory has no fix and every version at or above
// introduced is affected.
type interval struct{ introduced, fixed string }

// affects reports whether v falls inside any of the advisory's ranges.
func (a Advisory) affects(v string) bool {
	pinned := version.Parse(v)
	for _, r := range a.ranges {
		if r.introduced != "" && r.introduced != "0" && version.Compare(pinned, version.Parse(r.introduced)) < 0 {
			continue
		}
		if r.fixed != "" && version.Compare(pinned, version.Parse(r.fixed)) >= 0 {
			continue
		}
		return true
	}
	return false
}

// unversionedEcosystems names ecosystems OSV indexes but cannot order
// versions in, so a versioned query answers with nothing even when an
// advisory names a range covering the pin. Those are queried by package
// alone and the ranges are evaluated here.
var unversionedEcosystems = map[string]bool{
	"GitHub Actions": true,
}

// MinimumFix returns the smallest version above current that clears every
// advisory with a fix, or "" when none names a fix above current. Each
// advisory needs at least the fix closing the range current sits in, the
// answer is the largest of those, and a candidate that lands inside a later
// range of any advisory moves on to that range's fix. An advisory with no
// fix at all cannot be cleared by any version and is left out.
func MinimumFix(advisories []Advisory, current string) string {
	cur := version.Parse(current)
	var best string
	for _, a := range advisories {
		need := a.fixFor(cur)
		if need != "" && (best == "" || version.Compare(version.Parse(need), version.Parse(best)) > 0) {
			best = need
		}
	}
	// Each pass moves best strictly upward to a published fix, so the loop
	// ends after at most one step per range.
	for moved := best != ""; moved; {
		moved = false
		for _, a := range advisories {
			if !a.affects(best) {
				continue
			}
			if next := a.fixFor(version.Parse(best)); next != "" && version.Compare(version.Parse(next), version.Parse(best)) > 0 {
				best, moved = next, true
			}
		}
	}
	return best
}

// fixFor is the fix that closes the range v sits in, falling back to the
// smallest fix above v when the ranges are not known.
func (a Advisory) fixFor(v *version.Version) string {
	for _, r := range a.ranges {
		if r.fixed == "" {
			continue
		}
		if r.introduced != "" && r.introduced != "0" && version.Compare(v, version.Parse(r.introduced)) < 0 {
			continue
		}
		if version.Compare(v, version.Parse(r.fixed)) < 0 {
			return r.fixed
		}
	}
	var min string
	for _, fixed := range a.Fixed {
		if version.Compare(version.Parse(fixed), v) <= 0 {
			continue
		}
		if min == "" || version.Compare(version.Parse(fixed), version.Parse(min)) < 0 {
			min = fixed
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
	Version string            `json:"version,omitempty"`
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

	records := c.fetchVulns(ctx, uniqueIDs)

	out := make([][]Advisory, len(queries))
	for i, q := range queries {
		for _, id := range ids[i] {
			rec, ok := records[id]
			if !ok {
				continue
			}
			advisory := rec.toAdvisory(q.Package, q.Ecosystem)
			if unversionedEcosystems[q.Ecosystem] && !advisory.affects(q.Version) {
				continue
			}
			out[i] = append(out[i], advisory)
		}
	}
	return out, nil
}

// maxBatch is OSV's limit on queries per /v1/querybatch request.
const maxBatch = 1000

// fetchWorkers bounds how many advisory records are fetched at once. A
// lockfile with dozens of findings would otherwise pay one round trip each
// in sequence.
const fetchWorkers = 8

// fetchVulns fetches every advisory record in ids concurrently. A record
// that fails is logged and left out, so one bad fetch drops one advisory
// rather than the whole report.
func (c *Client) fetchVulns(ctx context.Context, ids map[string]struct{}) map[string]vulnRecord {
	logger := httpclient.LoggerFromContext(ctx)
	records := make(map[string]vulnRecord, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, fetchWorkers)
	for id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			rec, err := c.fetchVuln(ctx, id)
			if err != nil {
				logger.Warn("failed to fetch OSV advisory, skipping", "id", id, "error", err)
				return
			}
			mu.Lock()
			records[id] = rec
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return records
}

// batchQuery splits queries into requests OSV accepts and concatenates
// their answers in order.
func (c *Client) batchQuery(ctx context.Context, queries []Query) ([][]string, error) {
	out := make([][]string, 0, len(queries))
	for start := 0; start < len(queries); start += maxBatch {
		end := min(start+maxBatch, len(queries))
		ids, err := c.batchQueryChunk(ctx, queries[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return out, nil
}

func (c *Client) batchQueryChunk(ctx context.Context, queries []Query) ([][]string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("querying OSV", "resolver", "osv", "count", len(queries))

	req := batchRequest{Queries: make([]batchQuery, len(queries))}
	for i, q := range queries {
		req.Queries[i] = batchQuery{
			Package: batchQueryPackage{Name: q.Package, Ecosystem: q.Ecosystem},
			Version: q.Version,
		}
		if unversionedEcosystems[q.Ecosystem] {
			req.Queries[i].Version = ""
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
		Name      string `json:"name"`
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

// toAdvisory trims a full OSV record to the package and ecosystem a query
// asked about, since one advisory often covers several distro releases, or
// several packages of one ecosystem, with different fixed versions each.
// When no affected entry names the package (a name spelled differently),
// every entry of the ecosystem counts, as before.
func (r vulnRecord) toAdvisory(pkg, ecosystem string) Advisory {
	a := Advisory{
		ID:       r.ID,
		Summary:  r.Summary,
		Severity: r.severity(),
		URL:      r.url(),
	}
	named := false
	for _, affected := range r.Affected {
		if affected.Package.Ecosystem == ecosystem && strings.EqualFold(affected.Package.Name, pkg) {
			named = true
		}
	}
	for _, affected := range r.Affected {
		if affected.Package.Ecosystem != ecosystem {
			continue
		}
		if named && !strings.EqualFold(affected.Package.Name, pkg) {
			continue
		}
		for _, rng := range affected.Ranges {
			if rng.Type == "GIT" {
				continue
			}
			open := -1
			for _, event := range rng.Events {
				if event.Introduced != "" {
					a.ranges = append(a.ranges, interval{introduced: event.Introduced})
					open = len(a.ranges) - 1
				}
				if event.Fixed != "" {
					a.Fixed = append(a.Fixed, event.Fixed)
					if open >= 0 {
						a.ranges[open].fixed = event.Fixed
						open = -1
					}
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
