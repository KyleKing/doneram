package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

const repologyPath = "/api/v1/project/curl"

func repologyServer(t *testing.T, body string) string {
	t.Helper()
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		repologyPath: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		},
	})
	t.Cleanup(server.Close)
	return server.URL
}

func TestAPKResolverResolve(t *testing.T) {
	body := `[
		{"repo": "alpine_3_19", "version": "8.5.0-r0", "status": "newest"},
		{"repo": "alpine_3_18", "version": "8.2.1-r1", "status": "outdated"},
		{"repo": "debian_12", "version": "7.88.1-10", "status": "outdated"}
	]`
	r := NewAPKResolverWithBaseURL(&http.Client{}, repologyServer(t, body))

	got, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "8.5.0" {
		t.Errorf("Resolve() = %q, want 8.5.0", got)
	}

	pinned, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("8.2.#"))
	if err != nil {
		t.Fatalf("Resolve with pinned minor: %v", err)
	}
	if pinned != "8.2.1" {
		t.Errorf("Resolve() = %q, want 8.2.1", pinned)
	}
}

func TestAPKResolverNoAlpineVersions(t *testing.T) {
	r := NewAPKResolverWithBaseURL(&http.Client{}, repologyServer(t, `[{"repo": "debian_12", "version": "7.88.1-10"}]`))

	if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("Resolve should fail when no Alpine repo entries are present")
	}
}

func TestAPKResolverNoPatternMatch(t *testing.T) {
	r := NewAPKResolverWithBaseURL(&http.Client{}, repologyServer(t, `[{"repo": "alpine_3_19", "version": "8.5.0-r0"}]`))

	if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("9.#.#")); err == nil {
		t.Error("Resolve should fail when no version matches the pattern")
	}
}

func TestAPTResolverResolve(t *testing.T) {
	body := `[
		{"repo": "debian_12", "version": "7.88.1-10", "status": "newest"},
		{"repo": "ubuntu_24_04", "version": "8.5.0-2ubuntu1", "status": "newest"},
		{"repo": "alpine_3_19", "version": "8.9.0-r0", "status": "newest"}
	]`
	r := NewAPTResolverWithBaseURL(&http.Client{}, repologyServer(t, body))

	got, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "8.5.0" {
		t.Errorf("Resolve() = %q, want 8.5.0 (the Alpine entry must be ignored)", got)
	}
}

func TestAPTResolverNoDebianVersions(t *testing.T) {
	r := NewAPTResolverWithBaseURL(&http.Client{}, repologyServer(t, `[{"repo": "alpine_3_19", "version": "8.9.0-r0"}]`))

	if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("Resolve should fail when no Debian or Ubuntu entries are present")
	}
}

func TestAPTResolverNoPatternMatch(t *testing.T) {
	r := NewAPTResolverWithBaseURL(&http.Client{}, repologyServer(t, `[{"repo": "debian_12", "version": "7.88.1-10"}]`))

	if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("9.#.#")); err == nil {
		t.Error("Resolve should fail when no version matches the pattern")
	}
}

func TestRepologyResolversRejectErrorStatus(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		repologyPath: testutil.ErrorHandler(http.StatusServiceUnavailable),
	})
	defer server.Close()

	for name, r := range map[string]Resolver{
		"apk": NewAPKResolverWithBaseURL(&http.Client{}, server.URL),
		"apt": NewAPTResolverWithBaseURL(&http.Client{}, server.URL),
		"yum": NewYumResolverWithBaseURL(&http.Client{}, server.URL),
	} {
		if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#")); err == nil {
			t.Errorf("%s: Resolve should fail on a 503 response", name)
		}
	}
}

func TestRepologyResolversRejectMalformedJSON(t *testing.T) {
	url := repologyServer(t, `{"not":"an array"}`)

	for name, r := range map[string]Resolver{
		"apk": NewAPKResolverWithBaseURL(&http.Client{}, url),
		"apt": NewAPTResolverWithBaseURL(&http.Client{}, url),
		"yum": NewYumResolverWithBaseURL(&http.Client{}, url),
	} {
		if _, err := r.Resolve(context.Background(), "curl", parser.ParsePattern("#.#.#")); err == nil {
			t.Errorf("%s: Resolve should fail on a malformed response body", name)
		}
	}
}

func TestNormalizeAPKVersion(t *testing.T) {
	tests := map[string]string{
		"8.5.0-r0":  "8.5.0",
		"8.5.0-r12": "8.5.0",
		"8.5.0":     "8.5.0",
		"1.2.3-rc1": "1.2.3-rc1",
	}
	for in, want := range tests {
		if got := normalizeAPKVersion(in); got != want {
			t.Errorf("normalizeAPKVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDebianVersion(t *testing.T) {
	tests := map[string]string{
		"7.88.1-10":      "7.88.1",
		"8.5.0-2ubuntu1": "8.5.0",
		"8.5.0":          "8.5.0",
	}
	for in, want := range tests {
		if got := normalizeDebianVersion(in); got != want {
			t.Errorf("normalizeDebianVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
