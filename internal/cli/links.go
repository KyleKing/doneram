package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/resolver"
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

// tagLink is compareLink with each release version mapped to the git tag
// it was released under. A repository may tag "v1.2.3" or "1.2.3", and
// the version on disk does not say which; a link built on a guess opens
// GitHub's "nothing to compare" page. A version no tag matches gets no
// link rather than a broken one.
func tagLink(ctx context.Context, site engine.Site, current, latest string, lookup func(string) (resolver.Resolver, bool)) string {
	if current == "" || latest == "" || current == latest {
		return ""
	}
	if site.Locator.Resolver == "github-release" {
		if res, ok := lookup("github-release"); ok {
			if tagger, ok := res.(resolver.Tagger); ok {
				repo, _, _ := strings.Cut(site.ResolverName, "@")
				from, err1 := tagger.Tag(ctx, repo, current)
				to, err2 := tagger.Tag(ctx, repo, latest)
				if err1 != nil || err2 != nil || from == "" || to == "" {
					return ""
				}
				current, latest = from, to
			}
		}
	}
	return compareLink(site, current, latest)
}
