// Package provenance compares how two releases of a package were published,
// so an update whose build story changed is reviewed on its own rather than
// riding along with routine bumps. A release that lost the build
// attestation its predecessor carried, or that was built from a different
// source repository, or (npm, with no attestation either side) pushed by a
// different account, is the signature of a hijacked maintainer token or a
// compromised pipeline, and is the case a version-number diff cannot show.
//
// npm publishes SLSA provenance in the registry (`dist.attestations`), and
// PyPI publishes PEP 740 attestations through its integrity API. Both name
// the source repository and the workflow that built the release.
package provenance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/kyleking/doneram/internal/httpclient"
)

// Evidence is how one release was published.
type Evidence struct {
	// Attested is set when the release carries a build attestation.
	Attested bool
	// Repository and Workflow name what built it, when attested.
	Repository string
	Workflow   string
	// Publisher is the npm account that pushed the release.
	Publisher string
}

func (e Evidence) String() string {
	switch {
	case e.Attested && e.Workflow != "":
		return fmt.Sprintf("built by %s (%s)", e.Repository, e.Workflow)
	case e.Attested:
		return "built by " + e.Repository
	case e.Publisher != "":
		return "no build attestation, published by " + e.Publisher
	}
	return "no build attestation"
}

// Verdict is the comparison of two releases. A zero Verdict means nothing
// changed that needs a second look.
type Verdict struct {
	Changed bool
	Note    string
}

// Client fetches release metadata from npm and PyPI.
type Client struct {
	client   *http.Client
	npmBase  string
	pypiBase string

	mu    sync.Mutex
	cache map[string]evidenceResult
}

type evidenceResult struct {
	ev  Evidence
	err error
}

func New(client *http.Client) *Client {
	return NewWithBaseURLs(client, "https://registry.npmjs.org", "https://pypi.org")
}

func NewWithBaseURLs(client *http.Client, npmBase, pypiBase string) *Client {
	return &Client{client: client, npmBase: npmBase, pypiBase: pypiBase, cache: map[string]evidenceResult{}}
}

// Supported reports whether provenance can be compared for a resolver kind.
func Supported(kind string) bool {
	return kind == "npm" || kind == "pypi"
}

// Compare fetches both releases' evidence and judges the move from one to
// the other.
func (c *Client) Compare(ctx context.Context, kind, name, from, to string) (Verdict, error) {
	before, err := c.Evidence(ctx, kind, name, from)
	if err != nil {
		return Verdict{}, err
	}
	after, err := c.Evidence(ctx, kind, name, to)
	if err != nil {
		return Verdict{}, err
	}
	return Judge(name, from, to, before, after), nil
}

// Judge compares two releases' evidence.
func Judge(name, from, to string, before, after Evidence) Verdict {
	switch {
	case before.Attested && !after.Attested:
		return Verdict{true, fmt.Sprintf("%s %s has no build attestation; %s was %s", name, to, from, before)}
	case before.Attested && after.Attested && !strings.EqualFold(before.Repository, after.Repository):
		return Verdict{true, fmt.Sprintf("%s %s was built from %s; %s came from %s", name, to, after.Repository, from, before.Repository)}
	case !before.Attested && !after.Attested && before.Publisher != "" && after.Publisher != "" && before.Publisher != after.Publisher:
		return Verdict{true, fmt.Sprintf("%s %s was published by %s; %s by %s", name, to, after.Publisher, from, before.Publisher)}
	}
	return Verdict{}
}

// Evidence returns how one release was published, cached per run.
func (c *Client) Evidence(ctx context.Context, kind, name, version string) (Evidence, error) {
	key := kind + "\x00" + name + "\x00" + version
	c.mu.Lock()
	hit, ok := c.cache[key]
	c.mu.Unlock()
	if ok {
		return hit.ev, hit.err
	}
	var ev Evidence
	var err error
	switch kind {
	case "npm":
		ev, err = c.npm(ctx, name, version)
	case "pypi":
		ev, err = c.pypi(ctx, name, version)
	default:
		err = fmt.Errorf("no provenance source for %s", kind)
	}
	c.mu.Lock()
	c.cache[key] = evidenceResult{ev, err}
	c.mu.Unlock()
	return ev, err
}

func (c *Client) npm(ctx context.Context, name, version string) (Evidence, error) {
	var doc struct {
		NPMUser struct {
			Name string `json:"name"`
		} `json:"_npmUser"`
		Dist struct {
			Attestations *struct {
				URL        string `json:"url"`
				Provenance struct {
					PredicateType string `json:"predicateType"`
				} `json:"provenance"`
			} `json:"attestations"`
		} `json:"dist"`
	}
	escaped := strings.Replace(url.PathEscape(name), "%40", "@", 1)
	if err := c.getJSON(ctx, fmt.Sprintf("%s/%s/%s", c.npmBase, escaped, url.PathEscape(version)), "", &doc); err != nil {
		return Evidence{}, err
	}
	ev := Evidence{Publisher: doc.NPMUser.Name}
	att := doc.Dist.Attestations
	if att == nil || att.Provenance.PredicateType == "" {
		return ev, nil
	}
	ev.Attested = true

	// The attestation bundle names the repository and workflow; without it
	// the release still counts as attested, just without a source to
	// compare.
	var bundles struct {
		Attestations []struct {
			PredicateType string `json:"predicateType"`
			Bundle        struct {
				DSSEEnvelope struct {
					Payload string `json:"payload"`
				} `json:"dsseEnvelope"`
			} `json:"bundle"`
		} `json:"attestations"`
	}
	if att.URL == "" || c.getJSON(ctx, att.URL, "", &bundles) != nil {
		return ev, nil
	}
	for _, a := range bundles.Attestations {
		if !strings.Contains(a.PredicateType, "slsa.dev/provenance") {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(a.Bundle.DSSEEnvelope.Payload)
		if err != nil {
			continue
		}
		var statement struct {
			Predicate struct {
				BuildDefinition struct {
					ExternalParameters struct {
						Workflow struct {
							Repository string `json:"repository"`
							Path       string `json:"path"`
						} `json:"workflow"`
					} `json:"externalParameters"`
				} `json:"buildDefinition"`
			} `json:"predicate"`
		}
		if json.Unmarshal(payload, &statement) != nil {
			continue
		}
		wf := statement.Predicate.BuildDefinition.ExternalParameters.Workflow
		ev.Repository = strings.TrimPrefix(wf.Repository, "https://github.com/")
		ev.Workflow = wf.Path
		break
	}
	return ev, nil
}

func (c *Client) pypi(ctx context.Context, name, version string) (Evidence, error) {
	var release struct {
		URLs []struct {
			Filename string `json:"filename"`
		} `json:"urls"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/pypi/%s/%s/json", c.pypiBase, url.PathEscape(name), url.PathEscape(version)), "", &release); err != nil {
		return Evidence{}, err
	}
	// Every file of a release comes from one publish; the first one with
	// an attestation speaks for the release.
	for _, f := range release.URLs {
		var prov struct {
			Bundles []struct {
				Publisher struct {
					Repository string `json:"repository"`
					Workflow   string `json:"workflow"`
				} `json:"publisher"`
			} `json:"attestation_bundles"`
		}
		err := c.getJSON(ctx, fmt.Sprintf("%s/integrity/%s/%s/%s/provenance", c.pypiBase, url.PathEscape(name), url.PathEscape(version), url.PathEscape(f.Filename)),
			"application/vnd.pypi.integrity.v1+json", &prov)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return Evidence{}, err
		}
		if len(prov.Bundles) == 0 {
			continue
		}
		p := prov.Bundles[0].Publisher
		return Evidence{Attested: true, Repository: p.Repository, Workflow: p.Workflow}, nil
	}
	return Evidence{}, nil
}

var errNotFound = errors.New("not found")

func (c *Client) getJSON(ctx context.Context, u, accept string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.client.Do(req)
	var missing *httpclient.NotFoundError
	if errors.As(err, &missing) {
		return errNotFound
	}
	if err != nil {
		return fmt.Errorf("fetching %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: status %d", u, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}
