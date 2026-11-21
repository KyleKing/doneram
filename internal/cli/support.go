package cli

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/support"
)

// inferLifecycle covers the pins endoflife.date knows nothing about: a
// tool, action, or library whose release line looks unmaintained by its
// own release history. It judges the version the run leaves on disk (the
// update's target, or the pin when nothing newer is allowed), so a pin
// this run moves off a dead line is not reported. A site with its own
// `eol` product is answered by endoflife.date instead.
//
// Only a confident verdict counts as past end of life, the one
// --fail-on-eol gates on; the rest are reported as likely.
func inferLifecycle(ctx context.Context, baseDir string, results []engine.SiteResult, lookup func(string) (resolver.Resolver, bool), now time.Time) []pklLifecycleSummary {
	type job struct {
		hist         resolver.Historian
		pkg, version string
		tool, file   string
		line         int
		verdict      support.Verdict
	}
	var jobs []*job
	seen := map[string]bool{}
	for _, r := range results {
		if r.Err != nil || len(r.Matches) == 0 || r.Site.EOL != "" || r.Site.Command != "" {
			continue
		}
		res, ok := lookup(r.Site.Locator.Resolver)
		if !ok {
			continue
		}
		hist, ok := res.(resolver.Historian)
		if !ok {
			continue
		}
		version := r.Matches[0].Value
		if siteNeedsPatch(r) {
			version = r.Latest
		}
		// An action pin is "<sha> # v4.3.0"; the tag is the version.
		if _, tag, ok := strings.Cut(version, " # "); ok {
			version = tag
		}
		pkg := r.Site.ResolverName
		if pkg == "" {
			pkg = r.Site.Tool
		}
		key := r.Site.Locator.Resolver + "\x00" + pkg + "\x00" + version
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, &job{hist: hist, pkg: pkg, version: version, tool: r.Site.Tool, file: r.Matches[0].File, line: r.Matches[0].Line})
	}

	// History is one request per package, so a few run at once.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, j := range jobs {
		wg.Add(1)
		go func(j *job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			releases, err := j.hist.History(ctx, j.pkg)
			if err != nil {
				return
			}
			j.verdict = support.Infer(releases, j.version, now)
		}(j)
	}
	wg.Wait()

	var out []pklLifecycleSummary
	for _, j := range jobs {
		if !j.verdict.Ended {
			continue
		}
		out = append(out, pklLifecycleSummary{
			Source:   "release-history",
			File:     relTo(baseDir, j.file),
			Line:     j.line,
			Name:     j.tool,
			Product:  j.pkg,
			Cycle:    j.version,
			Past:     j.verdict.Confident,
			Inferred: true,
			Reason:   j.verdict.Reason,
		})
	}
	return out
}
