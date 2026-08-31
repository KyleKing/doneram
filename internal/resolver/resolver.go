package resolver

import (
	"context"

	"github.com/kyleking/doneram/internal/parser"
)

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
