package testutil

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp, string(body)
}

func TestNewMockServerRoutesHandlers(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{
		"/ok": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello"))
		},
	})
	defer server.Close()

	resp, body := get(t, server.URL+"/ok")
	if resp.StatusCode != http.StatusOK || body != "hello" {
		t.Errorf("got (%d, %q), want (200, \"hello\")", resp.StatusCode, body)
	}

	missing, _ := get(t, server.URL+"/missing")
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("unregistered path returned %d, want 404", missing.StatusCode)
	}
}

func TestRateLimitHandler(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{"/limited": RateLimitHandler(42)})
	defer server.Close()

	resp, _ := get(t, server.URL+"/limited")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want 42", got)
	}
}

func TestErrorHandler(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{"/boom": ErrorHandler(http.StatusBadGateway)})
	defer server.Close()

	resp, _ := get(t, server.URL+"/boom")
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestFixtureHandlerServesFixture(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{"/pkg": FixtureHandler("api/npm/express.json")})
	defer server.Close()

	resp, body := get(t, server.URL+"/pkg")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(body, "express") {
		t.Errorf("body does not look like the express fixture: %.60q", body)
	}
}

func TestFixtureHandlerMissingFixture(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{"/pkg": FixtureHandler("api/does-not-exist.json")})
	defer server.Close()

	resp, body := get(t, server.URL+"/pkg")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if !strings.Contains(body, "error loading fixture") {
		t.Errorf("body = %q, want an explanatory message", body)
	}
}

func TestLoadFixture(t *testing.T) {
	data, err := LoadFixture("api/npm/express.json")
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if len(data) == 0 {
		t.Error("LoadFixture returned no data")
	}

	if _, err := LoadFixture("api/does-not-exist.json"); err == nil {
		t.Error("LoadFixture should fail for a missing fixture")
	}
}

func TestFindProjectRoot(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	root := findProjectRoot(wd)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("findProjectRoot(%q) = %q, which has no go.mod", wd, root)
	}

	nested := filepath.Join(root, "internal", "testutil")
	if got := findProjectRoot(nested); got != root {
		t.Errorf("findProjectRoot(%q) = %q, want %q", nested, got, root)
	}
}

func TestFindProjectRootStopsAtFilesystemRoot(t *testing.T) {
	orphan := t.TempDir()

	got := findProjectRoot(orphan)
	if _, err := os.Stat(filepath.Join(got, "go.mod")); err == nil {
		return
	}
	if filepath.Dir(got) != got {
		t.Errorf("findProjectRoot(%q) = %q, want the filesystem root when no go.mod is found", orphan, got)
	}
}
