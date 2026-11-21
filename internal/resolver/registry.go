package resolver

import "net/http"

// Registry returns every resolver kind doneram can construct today, keyed
// by the name a locator's Resolver field names. A kind absent from this map
// is not implemented yet; callers report that rather than crashing.
func Registry(client *http.Client) map[string]Resolver {
	npm, pypi, github := NewNPMResolver(client), NewPyPIResolver(client), NewGitHubReleaseResolver(client)
	mise := NewMiseResolver()
	mise.GitHub, mise.NPM, mise.PyPI = github, npm, pypi
	return map[string]Resolver{
		"npm":            npm,
		"pypi":           pypi,
		"cargo":          NewCargoResolver(client),
		"composer":       NewComposerResolver(client),
		"rubygems":       NewRubyGemsResolver(client),
		"apk":            NewAPKResolver(client),
		"apt":            NewAPTResolver(client),
		"yum":            NewYumResolver(client),
		"docker":         NewDockerHubResolver(client),
		"dockerhub":      NewDockerHubResolver(client),
		"ghcr":           NewGHCRResolver(client),
		"mise":           mise,
		"github-release": github,
		"github-branch":  NewGitHubBranchResolver(client),
		"github-action":  NewGitHubActionResolver(client),
		"cdnjs":          NewCDNJSResolver(client),

		"terraform-provider": NewTerraformProviderResolver(client),
		"terraform-module":   NewTerraformModuleResolver(client),

		"aws-ssm": NewAWSSSMResolver(),
		"aws-ami": NewAWSAMIResolver(),
		"gke":     NewGKEResolver(),
	}
}
