package resolver

import (
	"context"
	"time"
)

// DefaultCooldown is how long a release must have existed before doneram
// offers it, matching Dependabot's own three-day default. It is the window
// in which a compromised or broken release is usually pulled.
const DefaultCooldown = 72 * time.Hour

type cooldownKey struct{}

// ContextWithCooldown carries the minimum release age for every resolver on
// this run. Zero offers a release the moment it appears.
func ContextWithCooldown(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, cooldownKey{}, d)
}

// cooldownCutoff is the newest publication time a release may carry and
// still be offered, or the zero time when nothing is held back.
func cooldownCutoff(ctx context.Context) time.Time {
	d, ok := ctx.Value(cooldownKey{}).(time.Duration)
	if !ok || d <= 0 {
		return time.Time{}
	}
	return time.Now().Add(-d)
}
