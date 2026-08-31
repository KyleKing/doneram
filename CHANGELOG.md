## v0.2.0 (2026-08-31)

### Feat

- track doneram's own pins in .doneram.pkl
- add composite action for pinned doneram installs
- add goreleaser config for cross-platform binaries
- **config**: let a tool override the default #.#.# constraint pattern
- **locator**: match a pattern across a window of consecutive lines
- **cli**: surface vulnerability findings in doneram check
- **vulncheck**: join OSV and image-scan findings onto resolved sites
- **vulnscan**: shell out to trivy or grype for image layer findings
- **osv**: add OSV.dev batch vulnerability client
- **resolver**: add GitHub Action tag-to-SHA resolver
- **cli**: patch pkl config sites in place with --apply
- **resolver**: add CDNJS resolver
- **resolver**: add GitHub release and branch resolvers
- **resolver**: add mise resolver
- **parser,engine**: add hold ceilings and command-resolver sites
- **cli**: report via locators when a repo has .doneram.pkl
- **locator**: load pkl config into locator sites
- **locator**: add locator matching and patch primitives
- improve logging/error handling and add http-based resolvers
- add batch processing
- expand resolvers and reporting
- add basic updater
- implement initial resolvers and tests
- init doner

### Fix

- **resolver**: fall back to tags when a repo has no GitHub Releases
- **resolver**: pick the highest matching release, not the first listed
- **cli**: report has_upgrades from detected drift, not only applied patches
- **ci**: migrate golangci-lint to the v2 config schema
- golangci-lint failures (#1)

### Refactor

- **parser**: compile doneram directives into locators
- **locator**: split resolver orchestration into internal/engine
- rename doner to doneram
