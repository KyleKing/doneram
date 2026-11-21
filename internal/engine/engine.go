// Package engine ties a locator to a resolver: finding a pin, enforcing its
// match count, and resolving its latest version. Both front ends (the
// Dockerfile directive parser and the pkl config loader) compile down to
// []Site and run through the same RunSites.
package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/pkg/version"
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

	// EOL is the endoflife.date product the pin's cycle is looked up in.
	EOL string

	// Dir is where Command runs, empty meaning the working directory. A
	// fleet run checks many clones from one process, so it cannot rely on
	// the working directory being the repo.
	Dir string
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
	// Behind is the resolver's answer when it was older than the pin on
	// disk, e.g. when --min-age held every release newer than the pin and
	// the newest one outside the window predates it. Latest is left empty
	// so the pin is never patched backwards.
	Behind string
	// Yanked is set when the registry has withdrawn the version on disk
	// (yanked on PyPI or crates.io, deprecated on npm), holding the reason
	// the registry gives, or "yanked" when it gives none.
	Yanked string
}

// ResolverLookup returns the resolver for a locator's Resolver kind, or
// false when that kind is not available yet.
type ResolverLookup func(kind string) (resolver.Resolver, bool)

// DefaultWorkers bounds how many sites resolve at once. Per-host limiting
// lives in the HTTP client, so this only caps total in-flight work.
const DefaultWorkers = 8

// Option configures a RunSites call.
type Option func(*options)

type options struct {
	workers int
}

// WithWorkers overrides how many sites resolve concurrently. A value below
// one falls back to DefaultWorkers.
func WithWorkers(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.workers = n
		}
	}
}

// RunSites finds and resolves every site, continuing past a single site's
// failure so one unavailable resolver or moved pin never hides the rest of
// the report. Sites resolve concurrently and results keep their input
// order; two sites asking the same resolver the same question share one
// answer.
func RunSites(ctx context.Context, sites []Site, lookup ResolverLookup, opts ...Option) []SiteResult {
	cfg := options{workers: DefaultWorkers}
	for _, opt := range opts {
		opt(&cfg)
	}

	run := &siteRunner{
		lookup:   lookup,
		resolved: newMemo[resolveKey, string](),
		detailed: newMemo[detailKey, string](),
		yanked:   newMemo[detailKey, string](),
		commands: newMemo[string, []byte](),
	}

	results := make([]SiteResult, len(sites))
	sem := make(chan struct{}, cfg.workers)
	var wg sync.WaitGroup

	for i, s := range sites {
		wg.Add(1)
		go func(i int, s Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = run.site(ctx, s)
		}(i, s)
	}

	wg.Wait()
	return results
}

type resolveKey struct {
	kind       string
	name       string
	constraint string
}

type detailKey struct {
	resolveKey
	current string
	latest  string
}

type siteRunner struct {
	lookup   ResolverLookup
	resolved *memo[resolveKey, string]
	detailed *memo[detailKey, string]
	yanked   *memo[detailKey, string]
	commands *memo[string, []byte]
}

func (r *siteRunner) site(ctx context.Context, s Site) SiteResult {
	result := SiteResult{Site: s}

	var matches []locator.Match
	if s.Locator.Glob != "" {
		found, err := locator.Find(s.Locator)
		if err != nil {
			result.Err = err
			return result
		}
		result.Matches = found

		if err := locator.CheckExpect(s.Locator, found); err != nil {
			result.Err = err
			return result
		}
		matches = found
	}

	if s.isCommand() {
		return r.commandSite(ctx, s, matches)
	}

	res, ok := r.lookup(s.Locator.Resolver)
	if !ok {
		result.Err = fmt.Errorf("resolver %q not available", s.Locator.Resolver)
		return result
	}

	key := resolveKey{
		kind:       s.Locator.Resolver,
		name:       s.resolverName(),
		constraint: s.constraint().String(),
	}
	latest, err := r.resolved.do(key, func() (string, error) {
		return res.Resolve(ctx, s.resolverName(), s.constraint())
	})
	if err != nil {
		result.Err = err
		return result
	}
	current := ""
	if len(matches) > 0 {
		current = matches[0].Value
	}
	r.checkYanked(ctx, res, key, current, &result)
	// Moving backwards is right only off a withdrawn version: the newest
	// release still standing is then the one to pin.
	if IsOlder(latest, current) && result.Yanked == "" {
		result.Behind = latest
		return result
	}
	latest = matchPrecision(latest, current)
	result.Latest = latest

	detailer, ok := res.(resolver.Detailer)
	if !ok {
		return result
	}

	detail, err := r.detailed.do(detailKey{key, current, latest}, func() (string, error) {
		return detailer.Detail(ctx, s.resolverName(), current, latest)
	})
	if err == nil {
		result.Detail = detail
	}

	return result
}

// plainVersion matches a value that pkg/version compares faithfully: up to
// three numeric segments, an optional leading v, and an optional suffix.
// A SHA, a date, or a four-segment version is left uncompared.
var plainVersion = regexp.MustCompile(`^v?\d+(\.\d+){0,2}(-\S+)?$`)

// comparableVersion extracts the version from a pin value, reading the
// trailing tag of a github-action "<sha> # v4.3.0" pin.
func comparableVersion(value string) (string, bool) {
	if i := strings.LastIndex(value, " # "); i != -1 {
		value = value[i+len(" # "):]
	}
	return value, plainVersion.MatchString(value)
}

// IsOlder reports whether latest is numerically below current. Only the
// numeric segments are compared: suffixes like alpine3.9 and alpine3.10 do
// not order as strings, and a wrong "older" verdict would silently block a
// real update, so a pin that differs only in its suffix is never held back.
func IsOlder(latest, current string) bool {
	l, ok := comparableVersion(latest)
	if !ok {
		return false
	}
	c, ok := comparableVersion(current)
	if !ok {
		return false
	}
	lv, cv := version.Parse(l), version.Parse(c)
	lv.Suffix, cv.Suffix = "", ""
	return version.Compare(lv, cv) < 0
}

// dottedNumber is a version made only of numeric segments, with an optional
// leading v: the shape whose precision a pin can choose on purpose.
var dottedNumber = regexp.MustCompile(`^v?\d+(\.\d+)*$`)

// matchPrecision trims latest to as many segments as current carries, so a
// pin written as "v2.12" (a floating minor that the consuming tool expands
// itself) moves to "v2.15" rather than being rewritten to "2.15.0". A pin
// that already names every segment, or any value that is not plainly
// dotted numbers, is returned unchanged. The v prefix follows current.
func matchPrecision(latest, current string) string {
	if !dottedNumber.MatchString(latest) || !dottedNumber.MatchString(current) {
		return latest
	}
	want := strings.Count(current, ".") + 1
	parts := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	if len(parts) <= want {
		return latest
	}
	trimmed := strings.Join(parts[:want], ".")
	if strings.HasPrefix(latest, "v") {
		trimmed = "v" + trimmed
	}
	return trimmed
}

// checkYanked records whether the pinned version has been withdrawn. A
// lookup failure is not the site's failure: the version answer stands, and
// yank status is advisory.
func (r *siteRunner) checkYanked(ctx context.Context, res resolver.Resolver, key resolveKey, current string, result *SiteResult) {
	checker, ok := res.(resolver.YankChecker)
	if !ok || current == "" {
		return
	}
	reason, err := r.yanked.do(detailKey{resolveKey: key, current: current}, func() (string, error) {
		reason, yanked, err := checker.Yanked(ctx, result.Site.resolverName(), current)
		if err != nil || !yanked {
			return "", err
		}
		if reason == "" {
			reason = "yanked"
		}
		return reason, nil
	})
	if err == nil {
		result.Yanked = reason
	}
}
