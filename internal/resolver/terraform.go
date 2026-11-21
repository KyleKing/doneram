package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

// TerraformResolver resolves the newest release of a Terraform provider
// ("hashicorp/aws") or registry module ("terraform-aws-modules/vpc/aws")
// from the public registry's version list.
type TerraformResolver struct {
	client  *http.Client
	baseURL string
	module  bool
}

func NewTerraformProviderResolver(client *http.Client) *TerraformResolver {
	return &TerraformResolver{client: client, baseURL: "https://registry.terraform.io"}
}

func NewTerraformModuleResolver(client *http.Client) *TerraformResolver {
	return &TerraformResolver{client: client, baseURL: "https://registry.terraform.io", module: true}
}

// WithBaseURL points the resolver at another registry, for tests.
func (r *TerraformResolver) WithBaseURL(baseURL string) *TerraformResolver {
	r.baseURL = baseURL
	return r
}

func (r *TerraformResolver) Name() string {
	if r.module {
		return "terraform-module"
	}
	return "terraform-provider"
}

func (r *TerraformResolver) Resolve(ctx context.Context, name string, pattern *parser.VersionPattern) (string, error) {
	var versions []string
	if r.module {
		if strings.Count(name, "/") != 2 {
			return "", fmt.Errorf("module %q is not namespace/name/provider", name)
		}
		doc, err := registryJSON[struct {
			Modules []struct {
				Versions []struct {
					Version string `json:"version"`
				} `json:"versions"`
			} `json:"modules"`
		}](ctx, r.client, fmt.Sprintf("%s/v1/modules/%s/versions", r.baseURL, name))
		if err != nil {
			return "", fmt.Errorf("fetching versions of module %s: %w", name, err)
		}
		for _, m := range doc.Modules {
			for _, v := range m.Versions {
				versions = append(versions, v.Version)
			}
		}
	} else {
		if strings.Count(name, "/") != 1 {
			return "", fmt.Errorf("provider %q is not namespace/name", name)
		}
		doc, err := registryJSON[struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		}](ctx, r.client, fmt.Sprintf("%s/v1/providers/%s/versions", r.baseURL, name))
		if err != nil {
			return "", fmt.Errorf("fetching versions of provider %s: %w", name, err)
		}
		for _, v := range doc.Versions {
			versions = append(versions, v.Version)
		}
	}

	var best string
	for _, v := range versions {
		if pattern != nil && !pattern.Matches(v) {
			continue
		}
		if best == "" || version.Compare(version.Parse(v), version.Parse(best)) > 0 {
			best = v
		}
	}
	if best == "" {
		return "", fmt.Errorf("no %s release of %s matches %v", r.Name(), name, pattern)
	}
	return best, nil
}

func (r *TerraformResolver) GetChangelog(ctx context.Context, pkg, from, to string) (string, error) {
	return "", nil
}

// registryJSON GETs a public registry document. Unlike getGitHubJSON it
// sends no credentials: a GITHUB_TOKEN must never reach a third party.
func registryJSON[T any](ctx context.Context, client *http.Client, url string) (T, error) {
	var out T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("status %d for %s", resp.StatusCode, url)
	}
	return out, json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&out)
}
