package cli

import (
	"context"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/vulncheck"
)

// securityFix records how a vulnerable pin's proposed version was chosen.
type securityFix struct {
	// Fixes is set when the proposed version clears the pin's advisories,
	// whether it came from the normal update or from the minimum fix.
	Fixes bool
	// Waived is set when the normal update was held by --min-age or did not
	// reach the fix, so the minimum patched version was taken instead.
	Waived bool
	// Err is why a minimum fix could not be turned into a pin value.
	Err error
}

// applySecurityFixes moves each vulnerable pin to the minimum version that
// clears its advisories whenever the normal update would not: the cooldown
// held every newer release, or the newest eligible release is still below
// the fix. A rushed security release is the one case where waiting out
// --min-age costs more than it protects, and the report says it was waived.
// A hold whose ceiling sits below the fix is never crossed.
func applySecurityFixes(ctx context.Context, results []engine.SiteResult, vulns []vulncheck.Result, lookup engine.ResolverLookup) []securityFix {
	fixes := make([]securityFix, len(results))
	pinned := make(map[string]string)

	for i := range results {
		r := &results[i]
		v := vulns[i]
		if r.Err != nil || r.Site.Locator.Glob == "" || len(r.Matches) == 0 {
			continue
		}
		if v.PatchedVersion == "" || v.HeldVulnerable {
			continue
		}
		if c := r.Site.Constraint; c != nil && !c.Matches(strings.TrimPrefix(v.PatchedVersion, "v")) {
			continue
		}

		if r.Latest != "" && r.Latest != r.Matches[0].Value && !engine.IsOlder(r.Latest, v.PatchedVersion) {
			fixes[i].Fixes = true
			continue
		}

		key := r.Site.Locator.Resolver + "\x00" + siteResolverName(r.Site) + "\x00" + v.PatchedVersion
		value, ok := pinned[key]
		if !ok {
			var err error
			value, err = pinValue(ctx, r.Site, r.Matches[0].Value, v.PatchedVersion, lookup)
			if err != nil {
				fixes[i].Err = err
				continue
			}
			pinned[key] = value
		}

		r.Latest = value
		r.Behind = ""
		fixes[i] = securityFix{Fixes: true, Waived: true}
	}
	return fixes
}

func siteResolverName(s engine.Site) string {
	if s.ResolverName != "" {
		return s.ResolverName
	}
	return s.Tool
}

// pinValue turns a bare version into what the site's capture group holds:
// a resolver that writes something other than the version (a github-action
// "<sha> # <tag>") supplies it, and otherwise the version is written with
// the same leading v the current pin carries.
func pinValue(ctx context.Context, site engine.Site, current, fixed string, lookup engine.ResolverLookup) (string, error) {
	if res, ok := lookup(site.Locator.Resolver); ok {
		if p, ok := res.(resolver.Pinner); ok {
			return p.Pin(ctx, siteResolverName(site), fixed)
		}
	}
	bare := strings.TrimPrefix(fixed, "v")
	if strings.HasPrefix(current, "v") {
		return "v" + bare, nil
	}
	return bare, nil
}
