package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const indexDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

// registry is a fake distribution API that requires a bearer token, like
// Docker Hub and GHCR do even for public images.
type registry struct {
	srv           *httptest.Server
	manifestHits  atomic.Int32
	tokenHits     atomic.Int32
	headHits      atomic.Int32
	getHits       atomic.Int32
	omitHeader    bool
	rejectHead    bool
	tokenField    string
	lastBasicPass atomic.Value
	body          string
}

func newRegistry(t *testing.T) *registry {
	t.Helper()
	r := &registry{tokenField: "token", body: `{"schemaVersion":2,"manifests":[]}`}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, req *http.Request) {
		r.tokenHits.Add(1)
		if _, pass, ok := req.BasicAuth(); ok {
			r.lastBasicPass.Store(pass)
		}
		if req.URL.Query().Get("scope") != "repository:library/python:pull" ||
			req.URL.Query().Get("service") != "registry.test" {
			http.Error(w, "bad scope", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintf(w, `{%q:"tok"}`, r.tokenField)
	})
	mux.HandleFunc("/v2/library/python/manifests/", func(w http.ResponseWriter, req *http.Request) {
		r.manifestHits.Add(1)
		if req.Header.Get("Authorization") != "Bearer tok" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/token",service="registry.test",scope="repository:library/python:pull"`, r.srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(req.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
			http.Error(w, "index type must be preferred", http.StatusBadRequest)
			return
		}
		if strings.TrimPrefix(req.URL.Path, "/v2/library/python/manifests/") != "3.13-slim" {
			http.NotFound(w, req)
			return
		}
		if req.Method == http.MethodHead {
			r.headHits.Add(1)
			if r.rejectHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
		} else {
			r.getHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		if !r.omitHeader {
			w.Header().Set("Docker-Content-Digest", indexDigest)
		}
		_, _ = w.Write([]byte(r.body))
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *registry) client() *Client {
	c := New(r.srv.Client()).WithRegistryURL("docker.io", r.srv.URL)
	c.getenv = func(string) string { return "" }
	return c
}

func mustParse(t *testing.T, s string) Ref {
	t.Helper()
	ref, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestResolveTokenFlow(t *testing.T) {
	for _, field := range []string{"token", "access_token"} {
		t.Run(field, func(t *testing.T) {
			reg := newRegistry(t)
			reg.tokenField = field
			got, err := reg.client().Resolve(context.Background(), mustParse(t, "python:3.13-slim"))
			if err != nil {
				t.Fatal(err)
			}
			if got != indexDigest {
				t.Errorf("Resolve = %q, want %q", got, indexDigest)
			}
			if n := reg.tokenHits.Load(); n != 1 {
				t.Errorf("token requests = %d, want 1", n)
			}
			if n := reg.getHits.Load(); n != 0 {
				t.Errorf("GET requests = %d, want 0 when HEAD has the header", n)
			}
		})
	}
}

func TestResolveReusesToken(t *testing.T) {
	reg := newRegistry(t)
	c := reg.client()
	ctx := context.Background()
	if _, err := c.Resolve(ctx, mustParse(t, "python:3.13-slim")); err != nil {
		t.Fatal(err)
	}
	// A different tag misses the digest cache but should reuse the token.
	if _, err := c.Resolve(ctx, mustParse(t, "python:missing")); err == nil {
		t.Fatal("want error for missing tag")
	} else if !strings.Contains(err.Error(), "python:missing") {
		t.Errorf("error %q does not name the image", err)
	}
	if n := reg.tokenHits.Load(); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}
}

func TestResolveBodyHash(t *testing.T) {
	reg := newRegistry(t)
	reg.omitHeader = true
	sum := sha256.Sum256([]byte(reg.body))
	want := "sha256:" + hex.EncodeToString(sum[:])

	got, err := reg.client().Resolve(context.Background(), mustParse(t, "python:3.13-slim"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want body hash %q", got, want)
	}
	if reg.headHits.Load() != 1 || reg.getHits.Load() != 1 {
		t.Errorf("HEAD=%d GET=%d, want one of each", reg.headHits.Load(), reg.getHits.Load())
	}
}

func TestResolveHeadRejected(t *testing.T) {
	reg := newRegistry(t)
	reg.rejectHead = true
	got, err := reg.client().Resolve(context.Background(), mustParse(t, "python:3.13-slim"))
	if err != nil {
		t.Fatal(err)
	}
	if got != indexDigest {
		t.Errorf("Resolve = %q, want GET header digest %q", got, indexDigest)
	}
}

func TestResolveCache(t *testing.T) {
	reg := newRegistry(t)
	c := reg.client()
	ref := mustParse(t, "python:3.13-slim")

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := c.Resolve(context.Background(), ref); err != nil || got != indexDigest {
				t.Errorf("Resolve = %q, %v", got, err)
			}
		}()
	}
	wg.Wait()

	before := reg.manifestHits.Load()
	// Same tag spelled differently shares the cache entry.
	for _, s := range []string{"python:3.13-slim", "docker.io/library/python:3.13-slim"} {
		if _, err := c.Resolve(context.Background(), mustParse(t, s)); err != nil {
			t.Fatal(err)
		}
	}
	if after := reg.manifestHits.Load(); after != before {
		t.Errorf("cached resolve made %d more requests", after-before)
	}
}

func TestResolveDigestOnly(t *testing.T) {
	reg := newRegistry(t)
	_, err := reg.client().Resolve(context.Background(), mustParse(t, "python@"+sampleDigest))
	if err == nil || !strings.Contains(err.Error(), "python@"+sampleDigest) {
		t.Errorf("want error naming the image, got %v", err)
	}
}

func TestResolveGitHubToken(t *testing.T) {
	reg := newRegistry(t)
	c := New(reg.srv.Client()).WithRegistryURL("ghcr.io", reg.srv.URL)
	c.getenv = func(k string) string {
		if k == "GITHUB_TOKEN" {
			return "gh-secret"
		}
		return ""
	}
	ref := Ref{Registry: "ghcr.io", Repository: "library/python", Tag: "3.13-slim"}
	if _, err := c.Resolve(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if got, _ := reg.lastBasicPass.Load().(string); got != "gh-secret" {
		t.Errorf("token request password = %q, want GITHUB_TOKEN", got)
	}
}

func TestResolveGitHubTokenNotSentElsewhere(t *testing.T) {
	reg := newRegistry(t)
	c := reg.client()
	c.getenv = func(string) string { return "gh-secret" }
	if _, err := c.Resolve(context.Background(), mustParse(t, "python:3.13-slim")); err != nil {
		t.Fatal(err)
	}
	if got, _ := reg.lastBasicPass.Load().(string); got != "" {
		t.Errorf("GITHUB_TOKEN sent to a non-GHCR registry")
	}
}

func TestResolveUnsupportedAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="private"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(srv.Client()).WithRegistryURL("registry.example.com", srv.URL)
	_, err := c.Resolve(context.Background(), mustParse(t, "registry.example.com/team/app:1"))
	if err == nil || !strings.Contains(err.Error(), "registry.example.com/team/app:1") {
		t.Errorf("want error naming the image, got %v", err)
	}
}

func TestParseBearerChallenge(t *testing.T) {
	got, ok := parseBearerChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:a/b:pull,push"`)
	if !ok {
		t.Fatal("not parsed")
	}
	want := map[string]string{
		"realm":   "https://auth.docker.io/token",
		"service": "registry.docker.io",
		"scope":   "repository:a/b:pull,push",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := parseBearerChallenge(`Basic realm="x"`); ok {
		t.Error("Basic challenge accepted")
	}
	if got, ok := parseBearerChallenge(`bearer realm=https://r.test/token, service=svc`); !ok || got["service"] != "svc" || got["realm"] != "https://r.test/token" {
		t.Errorf("unquoted challenge = %v, %v", got, ok)
	}
}
