package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// manifestTypes lists the index types first so a multi-arch tag resolves to
// its index digest, the one `docker buildx imagetools inspect` reports,
// rather than to one platform's manifest.
var manifestTypes = []string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}

const (
	// maxManifestBytes matches the 4 MiB manifest limit registries commonly
	// enforce; a larger body would be truncated and hash to the wrong digest.
	maxManifestBytes = 4 << 20
	maxTokenBytes    = 1 << 20
)

var errRateLimited = errors.New("registry rate limit reached (status 429): retry later")

// Client resolves tags to digests. It is safe for concurrent use.
type Client struct {
	http   *http.Client
	getenv func(string) string

	mu      sync.Mutex
	baseURL map[string]string // registry host -> base URL
	tokens  map[string]string // registry + "/" + repository -> bearer token
	digests map[string]string // registry + "/" + repository + ":" + tag -> digest
}

// New returns a Client that sends requests through client, or
// http.DefaultClient when nil.
func New(client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		http:    client,
		getenv:  os.Getenv,
		baseURL: map[string]string{},
		tokens:  map[string]string{},
		digests: map[string]string{},
	}
}

// WithRegistryURL maps a registry host (e.g. "registry-1.docker.io",
// "ghcr.io") to a base URL, for tests.
func (c *Client) WithRegistryURL(host, baseURL string) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL[normalizeRegistry(host)] = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) registryURL(host string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if u, ok := c.baseURL[host]; ok {
		return u
	}
	return "https://" + host
}

// Resolve returns the digest ("sha256:...") the reference's tag currently
// points to. Results are cached for the client's lifetime, so one run sees
// one answer per tag even when a file names the same image many times.
func (c *Client) Resolve(ctx context.Context, ref Ref) (string, error) {
	if ref.Tag == "" {
		return "", fmt.Errorf("resolving image %s: no tag to resolve", ref.display())
	}
	registry := normalizeRegistry(ref.Registry)
	if registry == "" {
		registry = dockerHub
	}
	key := registry + "/" + ref.Repository + ":" + ref.Tag

	c.mu.Lock()
	cached, ok := c.digests[key]
	c.mu.Unlock()
	if ok {
		return cached, nil
	}

	dgst, err := c.fetch(ctx, registry, ref)
	if err != nil {
		return "", fmt.Errorf("resolving image %s: %w", ref.display(), err)
	}

	c.mu.Lock()
	c.digests[key] = dgst
	c.mu.Unlock()
	return dgst, nil
}

// fetch tries HEAD first because it costs no body and, on Docker Hub, does
// not count against the anonymous pull rate limit.
func (c *Client) fetch(ctx context.Context, registry string, ref Ref) (string, error) {
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", c.registryURL(registry), ref.Repository, url.PathEscape(ref.Tag))

	resp, err := c.do(ctx, http.MethodHead, u, registry, ref.Repository)
	if err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if d := resp.Header.Get("Docker-Content-Digest"); digestPattern.MatchString(d) {
			return d, nil
		}
	case http.StatusNotFound:
		return "", fmt.Errorf("tag %q not found", ref.Tag)
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("access denied (status %d)", resp.StatusCode)
	case http.StatusTooManyRequests:
		return "", errRateLimited
	}

	// Some registries omit the header on HEAD or reject HEAD outright; the
	// body's hash is the digest by definition.
	resp, err = c.do(ctx, http.MethodGet, u, registry, ref.Repository)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("tag %q not found", ref.Tag)
	case http.StatusTooManyRequests:
		return "", errRateLimited
	default:
		return "", fmt.Errorf("registry returned status %d", resp.StatusCode)
	}
	if d := resp.Header.Get("Docker-Content-Digest"); digestPattern.MatchString(d) {
		return d, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading manifest: %w", err)
	}
	if len(body) > maxManifestBytes {
		return "", fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// do sends a manifest request, answering one bearer challenge if the
// registry asks for a token. A cached token that has expired gets the same
// treatment, so it is refreshed rather than reported as a failure.
func (c *Client) do(ctx context.Context, method, u, registry, repository string) (*http.Response, error) {
	tokenKey := registry + "/" + repository
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Accept", strings.Join(manifestTypes, ", "))
		c.mu.Lock()
		token := c.tokens[tokenKey]
		c.mu.Unlock()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching manifest: %w", err)
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return resp, nil
		}
		challenge := resp.Header.Get("WWW-Authenticate")
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxTokenBytes))
		_ = resp.Body.Close()

		params, ok := parseBearerChallenge(challenge)
		if !ok {
			return nil, fmt.Errorf("registry requires authentication this client does not support: %q", challenge)
		}
		if params["scope"] == "" {
			params["scope"] = "repository:" + repository + ":pull"
		}
		token, err = c.fetchToken(ctx, registry, params)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.tokens[tokenKey] = token
		c.mu.Unlock()
	}
}

type tokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// fetchToken requests a pull token from the challenge's realm. For ghcr.io a
// GITHUB_TOKEN lifts rate limits and opens private images, but a token
// without package scope is refused, so an authenticated failure falls back
// to anonymous, which is enough for public images.
func (c *Client) fetchToken(ctx context.Context, registry string, params map[string]string) (string, error) {
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" {
		return "", fmt.Errorf("invalid token realm %q", params["realm"])
	}
	q := realm.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	q.Set("scope", params["scope"])
	realm.RawQuery = q.Encode()

	if gh := c.getenv("GITHUB_TOKEN"); gh != "" && registry == "ghcr.io" && c.trustedRealm(registry, realm) {
		if token, err := c.requestToken(ctx, realm.String(), gh); err == nil {
			return token, nil
		}
	}
	return c.requestToken(ctx, realm.String(), "")
}

// trustedRealm keeps GITHUB_TOKEN from being sent to a realm on another host,
// since the realm comes from a response header.
func (c *Client) trustedRealm(registry string, realm *url.URL) bool {
	if realm.Host == registry {
		return true
	}
	base, err := url.Parse(c.registryURL(registry))
	return err == nil && realm.Host == base.Host
}

func (c *Client) requestToken(ctx context.Context, realm, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm, nil)
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	if password != "" {
		req.SetBasicAuth("token", password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching registry token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry token unavailable (status %d)", resp.StatusCode)
	}
	var body tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTokenBytes)).Decode(&body); err != nil {
		return "", fmt.Errorf("parsing registry token: %w", err)
	}
	if body.Token != "" {
		return body.Token, nil
	}
	if body.AccessToken != "" {
		return body.AccessToken, nil
	}
	return "", errors.New("registry token response had no token")
}

// parseBearerChallenge reads `Bearer realm="...",service="...",scope="..."`.
// Values are quoted because a scope may itself contain commas, as in
// "repository:a/b:pull,push".
func parseBearerChallenge(header string) (map[string]string, bool) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return nil, false
	}
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var val string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				return nil, false
			}
			val, after = after[1:end+1], after[end+2:]
		} else {
			val, after, _ = strings.Cut(after, ",")
			after = "," + after
		}
		params[key] = strings.TrimSpace(val)
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(after), ","))
	}
	if params["realm"] == "" {
		return nil, false
	}
	return params, true
}
