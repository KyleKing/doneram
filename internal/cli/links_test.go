package cli

import (
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
)

func TestCompareLink(t *testing.T) {
	action := engine.Site{
		ResolverName: "actions/checkout",
		Locator:      locator.Locator{Resolver: "github-action"},
	}
	got := compareLink(action, "abc # v4.2.2", "def # v5.0.0")
	want := "https://github.com/actions/checkout/compare/v4.2.2...v5.0.0"
	if got != want {
		t.Errorf("action link = %q, want %q", got, want)
	}

	release := engine.Site{
		ResolverName: "jdx/mise",
		Locator:      locator.Locator{Resolver: "github-release"},
	}
	if got := compareLink(release, "2025.1.0", "2025.2.0"); got != "https://github.com/jdx/mise/compare/2025.1.0...2025.2.0" {
		t.Errorf("release link = %q", got)
	}

	mise := engine.Site{ResolverName: "jq", Locator: locator.Locator{Resolver: "mise"}}
	if got := compareLink(mise, "1.7.1", "1.8.2"); got != "" {
		t.Errorf("mise link = %q, want none", got)
	}
	if got := compareLink(action, "abc # v4.2.2", "abc # v4.2.2"); got != "" {
		t.Errorf("unchanged pin link = %q, want none", got)
	}
}
