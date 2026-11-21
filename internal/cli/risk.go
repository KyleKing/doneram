package cli

import (
	"context"
	"fmt"

	"github.com/kyleking/doneram/internal/actionrisk"
	"github.com/kyleking/doneram/internal/engine"
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
