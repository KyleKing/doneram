package resolver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kyleking/doneram/internal/parser"
)

// TestResolveHoldsBackAFreshRelease proves the cooldown skips a release
// published inside the window and offers the previous one instead.
func TestResolveHoldsBackAFreshRelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v2.1.0", PublishedAt: time.Now().Add(-2 * time.Hour)},
		{TagName: "v2.0.0", PublishedAt: time.Now().Add(-30 * 24 * time.Hour)},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	res := NewGitHubReleaseResolverWithBaseURL(server.Client(), server.URL)

	got, err := res.Resolve(ContextWithCooldown(context.Background(), DefaultCooldown), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "2.0.0" {
		t.Errorf("Resolve = %q, want 2.0.0 held back from the two-hour-old release", got)
	}

	got, err = res.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "2.1.0" {
		t.Errorf("Resolve without a cooldown = %q, want 2.1.0", got)
	}
}
