package digest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/kyleking/doneram/internal/cloud"
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
	// run executes a cloud CLI for a private registry's login (ECR,
	// Artifact Registry), nil meaning cloud.Run.
	run cloud.Runner

	mu      sync.Mutex
	baseURL map[string]string // registry host -> base URL
	tokens  map[string]string // registry + "/" + repository -> bearer token
	digests map[string]string // registry + "/" + repository + ":" + tag -> digest
	noLogin map[string]error  // registry -> why its cloud login is unavailable
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
		run:     cloud.Run,
		baseURL: map[string]string{},
		tokens:  map[string]string{},
		digests: map[string]string{},
		noLogin: map[string]error{},
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
		c.mu.Lock()
		noLogin := c.noLogin[registry]
		c.mu.Unlock()
		if noLogin != nil {
			return "", noLogin
		}
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
		if strings.HasPrefix(token, "Basic ") {
			req.Header.Set("Authorization", token)
		} else if token != "" {
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
			// ECR answers with a Basic challenge: the login is the
			// credential itself, sent on every request.
			user, pass, err := c.cloudLogin(ctx, registry)
			if err != nil {
				return nil, err
			}
			if user == "" {
				return nil, fmt.Errorf("registry requires authentication this client does not support: %q", challenge)
			}
			c.mu.Lock()
			c.tokens[tokenKey] = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
			c.mu.Unlock()
			continue
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
		if token, err := c.requestToken(ctx, realm.String(), "token", gh); err == nil {
			return token, nil
		}
	}
	// Artifact Registry and gcr.io issue a pull token for gcloud's access
	// token, sent only to a realm on the registry's own host.
	// Without a login, a public image still pulls anonymously; a private
	// one is then refused and reported with the login's own error.
	if c.trustedRealm(registry, realm) {
		user, pass, err := c.cloudLogin(ctx, registry)
		switch {
		case errors.Is(err, cloud.ErrUnavailable):
			c.mu.Lock()
			c.noLogin[registry] = err
			c.mu.Unlock()
		case err != nil:
			return "", err
		case user != "":
			return c.requestToken(ctx, realm.String(), user, pass)
		}
	}
	return c.requestToken(ctx, realm.String(), "", "")
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

func (c *Client) requestToken(ctx context.Context, realm, user, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm, nil)
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	if password != "" {
		req.SetBasicAuth(user, password)
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

// ecrHost is a private ECR registry: <account>.dkr.ecr[-fips].<region>.amazonaws.com.
var ecrHost = regexp.MustCompile(`^\d{12}\.dkr\.ecr(?:-fips)?\.([a-z0-9-]+)\.amazonaws\.com(?:\.cn)?$`)

// cloudLogin returns the registry login a cloud CLI issues for a private
// registry it owns: `aws ecr get-login-password` for ECR, `gcloud auth
// print-access-token` for Artifact Registry and gcr.io. An empty user means
// the registry is not a cloud one; ErrUnavailable means it is, and this
// machine has no CLI or credentials for it.
func (c *Client) cloudLogin(ctx context.Context, registry string) (user, pass string, err error) {
	run := c.run
	if run == nil {
		run = cloud.Run
	}
	switch {
	case ecrHost.MatchString(registry):
		region := ecrHost.FindStringSubmatch(registry)[1]
		pass, err = run(ctx, "aws", "ecr", "get-login-password", "--region", region)
		return "AWS", pass, err
	case strings.HasSuffix(registry, "-docker.pkg.dev"), registry == "gcr.io", strings.HasSuffix(registry, ".gcr.io"):
		pass, err = run(ctx, "gcloud", "auth", "print-access-token")
		return "oauth2accesstoken", pass, err
	}
	return "", "", nil
}
