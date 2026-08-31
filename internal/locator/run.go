package locator

import (
	"context"
	"fmt"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

// Site pairs a Locator with what resolving its pin needs: which tool it
// belongs to, the name to resolve against (defaulting to Tool), and the
// version constraint (defaulting to "#.#.#", any version).
type Site struct {
	Tool         string
	Locator      Locator
	ResolverName string
	Constraint   *parser.VersionPattern
}

func (s Site) resolverName() string {
	if s.ResolverName != "" {
		return s.ResolverName
	}
	return s.Tool
}

func (s Site) constraint() *parser.VersionPattern {
	if s.Constraint != nil {
		return s.Constraint
	}
	return parser.ParsePattern("#.#.#")
}

// SiteResult is the outcome of finding and resolving one Site.
type SiteResult struct {
	Site    Site
	Matches []Match
	Latest  string
	Err     error
}

// ResolverLookup returns the resolver for a locator's Resolver kind, or
// false when that kind is not available yet.
type ResolverLookup func(kind string) (resolver.Resolver, bool)

// RunSites finds and resolves every site, continuing past a single site's
// failure so one unavailable resolver or moved pin never hides the rest of
// the report.
func RunSites(ctx context.Context, sites []Site, lookup ResolverLookup) []SiteResult {
	results := make([]SiteResult, 0, len(sites))

	for _, s := range sites {
		result := SiteResult{Site: s}

		matches, err := Find(s.Locator)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		result.Matches = matches

		if err := CheckExpect(s.Locator, matches); err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}

		r, ok := lookup(s.Locator.Resolver)
		if !ok {
			result.Err = fmt.Errorf("resolver %q not available", s.Locator.Resolver)
			results = append(results, result)
			continue
		}

		latest, err := r.Resolve(ctx, s.resolverName(), s.constraint())
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		result.Latest = latest

		results = append(results, result)
	}

	return results
}
