# Roadmap

What doneram does not do yet, and the decisions that bound it. What has
shipped is in [CHANGELOG.md](./CHANGELOG.md) and the README; the original
reasoning is in [generalizing.md](./generalizing.md).

## Scope

doneram tracks, updates, and validates dependency versions written as
literals in text files: anything it can find with a regex and resolve
against a registry. It also does the small jobs package managers leave
undone (raising a declared floor to what is locked, pinning a floating
reference, moving a project to its template's newest release) and hands
every install, resolve, and lock step back to the tool that owns it.

**Out of scope.** Dependency resolution, which uv, go, npm, and mise
already do. Running containers for vulnerability data: image contents are
read statically by a scanner (trivy or grype) when one is installed.
Repo housekeeping beyond pins (syncing with origin, triaging CI, waiting
on workflow runs) belongs to other tools.

## Next

1. **Cloud-published versions, read-only.** A pin whose newest version
   only the cloud provider knows, resolved with the SDK's default
   credentials and skipped with a note when there are none. doneram never
   writes to the account.
   - AWS: AMI IDs in Terraform, CloudFormation, and Packer (EC2
     `DescribeImages` by name and owner, or the public SSM parameters under
     `/aws/service/`), digests for private ECR images, and end of life
     from the provider itself (RDS and Aurora engine versions, EKS add-on
     versions per cluster version) in place of endoflife.date.
   - Google Cloud: GKE versions per release channel, Cloud SQL database
     versions, and Artifact Registry digests.
2. **Pre-release patterns.** `^` for release candidates, `&` for betas,
   `!` for alphas, documented in the README and not yet parsed.

## Open questions

- Whether GitHub's dependency graph raises alerts for action pins
  submitted as `pkg:githubactions/owner/repo@vX.Y.Z` (`--snapshot`). Not
  yet verified against a real advisory.
- Hold ceilings past an exclusive `<`: whether `<=`, ranges, or several
  clauses are worth supporting.
- An SBOM of a repo's own pins (CycloneDX from the config plus resolved
  versions), only if something consumes it.
- Caching advisory results. A weekly run against OSV's batch endpoint is
  cheap enough that it has not mattered.

## Not planned

- **Pulumi.** Its providers are npm, PyPI, and Go packages, already
  covered through lockfiles and declared ranges.
- **Stripe API versions.** Stripe publishes no machine-readable list.
- **Native PR creation.** doneram writes a JSON summary with grouped
  branches, titles, and bodies, and a workflow step opens the PRs.
- **Terraform advisories.** Providers and modules have no OSV ecosystem;
  their versions are tracked, their vulnerabilities are not.

## Decisions

- **Config is pkl**, evaluated with `pkl eval -f json`. pkl-go bindings
  only if the subprocess cost ever matters.
- **Comment directives and config compile to the same sites.** One engine:
  a Dockerfile directive and a config entry get the same resolution,
  cooldown, advisories, provenance, and PR groups.
- **Resolvers shell out to mise** where a tool is in its registry, and use
  the upstream registry natively otherwise.
- **The minimum release age defaults to three days**, matching Dependabot.
  A version that fixes a published advisory skips the wait, and the PR
  says so. A hold is never crossed for a fix.
- **A hold carries its reason and a ceiling.** `hold[reason; <3.0.0]` keeps
  taking 2.x and reports the reason every run, so it cannot rot silently.
- **Gates are opt-in** (`--fail-on-drift`, `--fail-on-vuln`,
  `--fail-on-eol`, `--fail-on-unpinned`), so a repo chooses between a red
  CI job and a pull request.
- **Merging is GitHub's job.** doneram splits changes into `updates`,
  `review-<tool>`, and `failing-<tool>` PRs; rulesets decide what merges.
- **End of life and runtime moves are reported, never bumped.** A runtime
  or model change is a migration, not a pin update.
- **A template owns the pins in the files it renders.** They move in the
  template and reach projects through `copier update`.
