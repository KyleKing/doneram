package cli

import (
	"context"
	"fmt"

	"github.com/kyleking/doneram/internal/actionrisk"
	"github.com/kyleking/doneram/internal/changelog"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/provenance"
)

// assessRisks judges each github-action site moving across a major version
// against how the workflow uses it. A site with no major bump gets a zero
// Assessment; one whose metadata could not be read is marked for review
// with the reason, since "unknown" must never read as "safe".
func assessRisks(ctx context.Context, results []engine.SiteResult, client *actionrisk.Client) []actionrisk.Assessment {
	out := make([]actionrisk.Assessment, len(results))
	for i, r := range results {
		if r.Err != nil || r.Site.Locator.Resolver != "github-action" || r.Latest == "" || len(r.Matches) == 0 {
			continue
		}
		from, to := actionTag(r.Matches[0].Value), actionTag(r.Latest)
		if from == "" || to == "" || !actionrisk.IsMajor(from, to) {
			continue
		}

		var usages []actionrisk.Usage
		for _, m := range r.Matches {
			u, err := actionrisk.UsageAt(m.File, m.Line)
			if err != nil {
				continue
			}
			usages = append(usages, u)
		}

		a, err := client.Assess(ctx, siteResolverName(r.Site), from, to, usages)
		if err != nil {
			out[i] = actionrisk.Assessment{
				Level: actionrisk.Review,
				Notes: []string{fmt.Sprintf("could not compare action interfaces: %v", err)},
			}
			continue
		}
		out[i] = a
	}
	return out
}

// assessLibraryRisks grades each PyPI, npm, and cargo site moving across a
// major by its release notes against this repo's code, filling the slots
// assessRisks left empty. Like actions, a grade that could not be read is
// review, never safe.
func assessLibraryRisks(ctx context.Context, results []engine.SiteResult, risks []actionrisk.Assessment, client *changelog.Client, repoDir string) {
	for i, r := range results {
		kind := r.Site.Locator.Resolver
		if r.Err != nil || r.Latest == "" || len(r.Matches) == 0 || risks[i].Level != "" || !changelog.IsMajor(r.Matches[0].Value, r.Latest) {
			continue
		}
		from := r.Matches[0].Value
		if kind == "terraform-provider" || kind == "terraform-module" {
			// Registry release notes are free text with no stable home, and
			// a major usually ships an upgrade guide that needs reading.
			risks[i] = actionrisk.Assessment{Level: actionrisk.Review, Notes: []string{fmt.Sprintf("%s %s → %s crosses a major; read its upgrade guide", siteResolverName(r.Site), from, r.Latest)}}
			continue
		}
		if !changelog.Supported(kind) {
			continue
		}
		a, err := client.Assess(ctx, kind, siteResolverName(r.Site), from, r.Latest, repoDir)
		if err != nil {
			risks[i] = actionrisk.Assessment{Level: actionrisk.Review, Notes: []string{fmt.Sprintf("could not read release notes: %v", err)}}
			continue
		}
		risks[i] = actionrisk.Assessment{Level: actionrisk.Level(a.Level), Notes: a.Notes}
	}
}

// checkProvenance compares how each npm or PyPI site's proposed release
// was published against its current one. A changed build story (lost
// attestation, different source repository, different npm publisher)
// returns its note, which sends the bump to a review PR of its own.
func checkProvenance(ctx context.Context, results []engine.SiteResult, client *provenance.Client) []string {
	out := make([]string, len(results))
	for i, r := range results {
		kind := r.Site.Locator.Resolver
		if r.Err != nil || !provenance.Supported(kind) || r.Latest == "" || len(r.Matches) == 0 || r.Latest == r.Matches[0].Value {
			continue
		}
		v, err := client.Compare(ctx, kind, siteResolverName(r.Site), r.Matches[0].Value, r.Latest)
		if err != nil || !v.Changed {
			continue
		}
		out[i] = v.Note
	}
	return out
}
