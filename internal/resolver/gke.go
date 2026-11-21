package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kyleking/doneram/internal/cloud"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

// GKEResolver reads the GKE versions a release channel offers in a
// location. resolverName is "<location>/<CHANNEL>" ("us-central1/REGULAR"),
// optionally with "@<project>" when gcloud has no default project; with no
// channel, the location's valid master versions are used. A GKE
// version carries a build suffix ("1.33.5-gke.1080000"), so a pattern with
// no suffix of its own accepts any.
type GKEResolver struct{ Run cloud.Runner }

func NewGKEResolver() *GKEResolver { return &GKEResolver{Run: cloud.Run} }

func (*GKEResolver) Name() string { return "gke" }

type gkeServerConfig struct {
	ValidMasterVersions []string `json:"validMasterVersions"`
	Channels            []struct {
		Channel       string   `json:"channel"`
		ValidVersions []string `json:"validVersions"`
	} `json:"channels"`
}

func (r *GKEResolver) Resolve(ctx context.Context, name string, pattern *parser.VersionPattern) (string, error) {
	spec, project, _ := strings.Cut(name, "@")
	location, channel, _ := strings.Cut(spec, "/")
	if location == "" {
		return "", fmt.Errorf("gke resolverName %q: want <location>[/<CHANNEL>][@project]", name)
	}
	args := []string{"container", "get-server-config", "--location", location, "--format", "json"}
	if project != "" {
		args = append(args, "--project", project)
	}
	out, err := r.Run(ctx, "gcloud", args...)
	if err != nil {
		return "", err
	}
	var cfg gkeServerConfig
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return "", fmt.Errorf("parsing GKE server config: %w", err)
	}
	candidates := cfg.ValidMasterVersions
	if channel != "" {
		candidates = nil
		for _, c := range cfg.Channels {
			if strings.EqualFold(c.Channel, channel) {
				candidates = c.ValidVersions
			}
		}
	}
	p := pattern
	if p != nil && p.Suffix == "" {
		anySuffix := *p
		anySuffix.Suffix = "*"
		p = &anySuffix
	}
	var matching []string
	for _, v := range candidates {
		if p == nil || p.Matches(v) {
			matching = append(matching, v)
		}
	}
	if len(matching) == 0 {
		return "", fmt.Errorf("no GKE version in %s matches %v", name, pattern)
	}
	return version.Latest(matching), nil
}

func (*GKEResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}
