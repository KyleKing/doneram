// Package engine ties a locator to a resolver: finding a pin, enforcing its
// match count, and resolving its latest version. Both front ends (the
// Dockerfile directive parser and the pkl config loader) compile down to
// []Site and run through the same RunSites.
package engine

import (
	"context"
	"fmt"

	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

// Site pairs a Locator with what resolving its pin needs: which tool it
// belongs to, the name to resolve against (defaulting to Tool), and the
// version constraint (defaulting to "#.#.#", any version).
//
// A site with Command set resolves its latest version by parsing that
// command's output instead of querying a registry. It may still carry a
// Locator, in which case the file holds the current value and is what gets
// patched; without one the command's own output supplies both.
type Site struct {
	Tool         string
	Locator      locator.Locator
	ResolverName string
	Constraint   *parser.VersionPattern

	Command        string
	CommandPattern string

	// Ecosystem names the OSV ecosystem a site's pin belongs to (e.g.
	// "PyPI", "Alpine:v3.19"), overriding the default derived from
	// Locator.Resolver. Empty skips OSV lookup for the site.
	Ecosystem string
}

func (s Site) isCommand() bool {
	return s.Command != ""
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
	Matches []locator.Match
	Latest  string
	Err     error
	// Detail is extra report text a resolver.Detailer supplies alongside a
	// resolved version, e.g. a branch-tracking SHA's drift and age.
	Detail string
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

		var matches []locator.Match
		if s.Locator.Glob != "" {
			found, err := locator.Find(s.Locator)
			if err != nil {
				result.Err = err
				results = append(results, result)
				continue
			}
			result.Matches = found

			if err := locator.CheckExpect(s.Locator, found); err != nil {
				result.Err = err
				results = append(results, result)
				continue
			}
			matches = found
		}

		if s.isCommand() {
			results = append(results, runCommandSite(ctx, s, matches))
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

		if detailer, ok := r.(resolver.Detailer); ok {
			current := ""
			if len(matches) > 0 {
				current = matches[0].Value
			}
			if detail, err := detailer.Detail(ctx, s.resolverName(), current, latest); err == nil {
				result.Detail = detail
			}
		}

		results = append(results, result)
	}

	return results
}
