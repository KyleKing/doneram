package resolver

import "net/http"

// Registry returns every resolver kind doneram can construct today, keyed
// by the name a locator's Resolver field names. A kind absent from this map
// is not implemented yet; callers report that rather than crashing.
func Registry(client *http.Client) map[string]Resolver {
	return map[string]Resolver{
		"npm":            NewNPMResolver(client),
		"pypi":           NewPyPIResolver(client),
		"cargo":          NewCargoResolver(client),
		"composer":       NewComposerResolver(client),
		"rubygems":       NewRubyGemsResolver(client),
		"apk":            NewAPKResolver(client),
		"apt":            NewAPTResolver(client),
		"yum":            NewYumResolver(client),
		"docker":         NewDockerHubResolver(client),
		"dockerhub":      NewDockerHubResolver(client),
		"ghcr":           NewGHCRResolver(client),
		"mise":           NewMiseResolver(),
		"github-release": NewGitHubReleaseResolver(client),
		"github-branch":  NewGitHubBranchResolver(client),
	}
}
