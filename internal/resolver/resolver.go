package resolver

import (
	"context"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/support"
)

// Tagger is implemented by a Resolver whose versions come from git tags,
// to name the exact tag a version was released under ("v1.2.3" or
// "1.2.3"), which a compare link needs.
type Tagger interface {
	Tag(ctx context.Context, pkg, version string) (string, error)
}

// Historian is implemented by a Resolver that can list a package's
// releases with their publish dates, which is what inferring an end of
// life from release history needs.
type Historian interface {
	History(ctx context.Context, pkg string) ([]support.Release, error)
}

type Resolver interface {
	Name() string
	Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error)
	GetChangelog(ctx context.Context, pkg string, from, to string) (string, error)
}

// Detailer is implemented by a Resolver whose report needs more than a
// resolved version string, e.g. a branch-tracking SHA's commit drift and
// age, or a disagreement with another source for the same package.
type Detailer interface {
	Detail(ctx context.Context, pkg string, current, latest string) (string, error)
}

// Pinner is implemented by a Resolver whose pin value is not the bare
// version string, e.g. a github-action pin that writes "<sha> # v4.3.0".
// Pin turns a specific version (typically the minimum release clearing an
// advisory) into the value the site's capture group should hold.
type Pinner interface {
	Pin(ctx context.Context, pkg, version string) (string, error)
}
