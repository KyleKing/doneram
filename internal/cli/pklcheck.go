package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kyleking/doneram/internal/config"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/resolver"
)

const doneramConfigName = ".doneram.pkl"

// findDoneramConfig looks for a .doneram.pkl in the current directory,
// which `check` prefers over the default ./Dockerfile per doneram.md.
func findDoneramConfig() (string, bool) {
	info, err := os.Stat(doneramConfigName)
	if err != nil || info.IsDir() {
		return "", false
	}
	return doneramConfigName, true
}

// runCheckPkl loads a repo's .doneram.pkl and reports on every site it
// declares, routing each through the same locator/resolver engine a
// Dockerfile directive compiles into.
func runCheckPkl(ctx context.Context, path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}

	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	sites := cfg.Sites(baseDir)
	if len(sites) == 0 {
		fmt.Printf("%s declares no tools\n", path)
		return nil
	}

	httpClient := httpclient.New(httpclient.DefaultConfig())
	registry := resolver.Registry(httpClient)
	lookup := func(kind string) (resolver.Resolver, bool) {
		r, ok := registry[kind]
		return r, ok
	}

	results := engine.RunSites(ctx, sites, lookup)

	var mismatches int
	for _, result := range results {
		reportSiteResult(result)
		var mismatch *locator.MismatchError
		if errors.As(result.Err, &mismatch) {
			mismatches++
		}
	}

	fmt.Printf("\nChecked %d site(s) across %d tool(s)\n", len(results), len(cfg.Tools))

	if mismatches > 0 {
		return fmt.Errorf("%d site(s) failed match-count validation", mismatches)
	}

	return nil
}

func reportSiteResult(result engine.SiteResult) {
	var mismatch *locator.MismatchError
	switch {
	case errors.As(result.Err, &mismatch):
		fmt.Printf("✗ %s (%s): %v\n", result.Site.Tool, result.Site.Locator.Glob, mismatch)
		for _, c := range mismatch.Candidates {
			fmt.Printf("    candidate: %s:%d -> %s\n", c.File, c.Line, c.Value)
		}
	case result.Err != nil:
		fmt.Printf("? %s (%s): %v\n", result.Site.Tool, result.Site.Locator.Glob, result.Err)
	case len(result.Matches) > 0 && result.Latest != result.Matches[0].Value:
		fmt.Printf("→ %s: %s -> %s\n", result.Site.Tool, result.Matches[0].Value, result.Latest)
	default:
		fmt.Printf("✓ %s: up to date (%s)\n", result.Site.Tool, result.Latest)
	}

	if hold := result.Site.Constraint; hold != nil && hold.HoldReason != "" {
		fmt.Printf("    held: %s (ceiling <%s)\n", hold.HoldReason, hold.Ceiling)
	}
	if result.Detail != "" {
		fmt.Printf("    %s\n", result.Detail)
	}
}
