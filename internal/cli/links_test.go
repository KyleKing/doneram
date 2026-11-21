package cli

import (
	"context"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/resolver"
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

type stubTagger struct {
	fixedResolver
	tags map[string]string
}

func (s stubTagger) Tag(_ context.Context, _, version string) (string, error) {
	return s.tags[version], nil
}

func TestTagLinkUsesRealTags(t *testing.T) {
	site := engine.Site{ResolverName: "acme/tool", Locator: locator.Locator{Resolver: "github-release"}}
	lookup := func(tags map[string]string) func(string) (resolver.Resolver, bool) {
		return func(string) (resolver.Resolver, bool) { return stubTagger{tags: tags}, true }
	}
	if got := tagLink(context.Background(), site, "1.2.0", "1.3.0", lookup(map[string]string{"1.2.0": "v1.2.0", "1.3.0": "v1.3.0"})); got != "https://github.com/acme/tool/compare/v1.2.0...v1.3.0" {
		t.Errorf("v-tagged = %q", got)
	}
	if got := tagLink(context.Background(), site, "1.2.0", "1.3.0", lookup(map[string]string{"1.2.0": "1.2.0", "1.3.0": "1.3.0"})); got != "https://github.com/acme/tool/compare/1.2.0...1.3.0" {
		t.Errorf("bare-tagged = %q", got)
	}
	if got := tagLink(context.Background(), site, "1.2.0", "1.3.0", lookup(map[string]string{"1.3.0": "v1.3.0"})); got != "" {
		t.Errorf("untagged version linked: %q", got)
	}
}
