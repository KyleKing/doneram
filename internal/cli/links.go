package cli

import (
	"fmt"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
)

// compareLink builds the GitHub compare URL between what is pinned and what
// is offered, so a review reads the diff rather than trusting two version
// strings. Only the GitHub resolvers name a repo; everything else gets no
// link.
func compareLink(site engine.Site, current, latest string) string {
	if current == "" || latest == "" || current == latest {
		return ""
	}

	repo := site.ResolverName
	if strings.Count(repo, "/") != 1 {
		return ""
	}

	switch site.Locator.Resolver {
	case "github-action":
		current, latest = actionTag(current), actionTag(latest)
	case "github-release", "github-branch":
		repo, _, _ = strings.Cut(repo, "@")
	default:
		return ""
	}

	if current == "" || latest == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", repo, current, latest)
}

// actionTag pulls the tag out of a github-action pin, whose value is the
// composite "<sha> # <tag>" the locator patches as one unit.
func actionTag(pin string) string {
	_, tag, ok := strings.Cut(pin, "# ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(tag)
}
