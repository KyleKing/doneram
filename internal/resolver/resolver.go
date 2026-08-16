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
