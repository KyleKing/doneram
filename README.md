# Doneram

Doneram keeps pinned versions current. It finds a pin, asks the right
registry what the newest matching version is, patches the file, and
optionally runs a command to prove the update didn't break anything.

A pin is any version literal doneram can find with a regex: a Dockerfile
`FROM` tag, a mise tool version, a GitHub Action SHA, a CDN URL, a bash
variable default. Dockerfiles get a comment syntax because a `FROM` line
reads better with a directive above it than with an entry in a config file.
Everything else is declared in the repo's config.

Beyond declared pins, doneram scans lockfiles for advisories, raises
declared floors and relocks, pins floating references, reports end-of-life
runtimes, and moves projects to their copier template's newest release.
[ROADMAP.md](./ROADMAP.md) draws the in-scope and out-of-scope line and lists
what is next. The original design reasoning is in
[generalizing.md](./generalizing.md).

## Installation

### Homebrew

```bash
brew install kyleking/tap/doneram
```

### From source

```bash
go install github.com/kyleking/doneram/cmd/doneram@latest
```

### Binary releases

Download from [GitHub Releases](https://github.com/kyleking/doneram/releases).

## Comment directives

```dockerfile
# doneram: <package-name>:<version-pattern>
```

The directive applies to the `FROM` or `COPY --from` line after it. Every
Dockerfile in the repo is read (`Dockerfile`, `Dockerfile.*`, and
`*.Dockerfile`, outside vendored, build, and test-fixture directories), and
each directive becomes a site like any declared in config, so it gets the
same release-age wait, advisory check, provenance check, and PR grouping.

```dockerfile
# doneram: uv:0.9.#
FROM ghcr.io/astral-sh/uv:0.9.26 AS uv

# doneram: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 AS builder
```

### Version patterns

`#` is a wildcard for a version segment, `*` for a suffix.

- `3.13.#` pins major and minor, taking patch updates (3.13.11 to 3.13.12)
- `3.#.#` pins major, taking minor and patch (3.13.11 to 3.14.0)
- `#.#.#` takes any version
- `3.13.11` is fully pinned and never moves
- `alpine3.#` and `-alpine*` match a suffix (alpine3.21 to alpine3.22)

Pre-release suffixes are planned: `^` for release candidates, `&` for
betas, `!` for alphas.

### Not updating something

```dockerfile
# doneram: ignore
FROM legacy-image:1.0.0
```

`ignore` skips the pin entirely. A hold is different: it keeps taking
updates below a ceiling, so a known breaking change blocks only the versions
past it.

```dockerfile
# doneram: hold[cgo build breaks on 3.0; <3.0.0]
```

Held pins appear in the report with their reason, so a hold added for a
temporary problem can't rot silently.

## Discovery

Most pins need no config. doneram finds and checks these on its own:

- SHA-pinned actions written as `uses: owner/repo@<sha> # v1.2.3` in
  `.github/workflows/`, `.github/actions/*/action.yml`, and a root
  `action.yml`. A SHA without its version comment is left alone; a
  mutable tag is pinned first (below).
- `# doneram:` directives in Dockerfiles (above).
- Plain versions under `[tools]` in `mise.toml`, and in `.tool-versions`.
  `latest`, `lts`, and arrays are not pins and are skipped.

### Terraform

Provider versions in `required_providers` and registry module versions are
pins too. An exact version (`"5.1.2"`) moves to the newest release; a
pessimistic constraint (`"~> 5.0"`) stays inside the range it names while
its floor rises at the precision it was written (`~> 5.0` to `~> 5.51`).
Open ranges (`">= 4.0"`), local paths, and git sources are left alone, and
a major goes to a review PR, since it usually comes with an upgrade guide.
`.terraform.lock.hcl` is Terraform's to rewrite: set `afterPatch = "terraform
init -upgrade -backend=false"` to have it follow.

### Copier templates

A project generated from a [copier](https://copier.readthedocs.io) template
is pinned to a template release by `_commit` in `.copier-answers.yml`.
doneram reads it (and `.copier-answers.*.yml` beside it or one directory
down), finds the template repository's newest release on GitHub, and on
`update` runs `copier update --defaults --skip-answered --vcs-ref <tag>`,
which re-renders the template and merges it with local edits. Conflicts are
left as markers or `.rej` files for the PR's reviewer.

**The template owns its files' pins.** A file the template keeps rendering
(a workflow, a shared mise config) gets its pins from the template, so
doneram leaves them alone in the project and reports how many it left:

```
  13 pin(s) left to template gh:acme/go-template, which releases them to this project
```

Bump them once in the template, release it, and every project picks them
up as a copier update, instead of each project patching the same workflow
line and then merging against the next update. doneram lists the template
at `_commit` (`_subdirectory`, `.jinja` suffixes, and conditional names
like `{% if flag %}dir{% endif %}` are understood) and leaves to the
project every file the template does not render, plus its
`_skip_if_exists` files, which the template writes once and never again.
Lockfiles, `go.mod`, and other project-owned files are still checked here.
A pin the project wants to move anyway can be declared in its config,
since declared sites are always checked.

**Tasks run only for a trusted template.** copier refuses to update a
template with `_tasks` or `_migrations` unless given `--trust`. doneram
passes it when the template lives under the same GitHub account as the
project, since that code is the owner's own, or when the template's
`_src_path` is listed:

```pkl
trustTemplates = new { "gh:acme/shared-template" }
```

Any other template whose update needs tasks fails, and the report gives
the command to run by hand.

In the template repository itself, declare the pins in its `.jinja`
sources with an `afterPatch` that re-renders, so the weekly PR there is
the one place a version moves. Each project's own weekly run then proposes
the copier update, and `doneram fleet --apply` can make it across local
checkouts.

### Pinning floating references

A tag or version prefix can point at new code without a commit in your
repo. `doneram pin` rewrites each one as an exact pin, and `update` does the
same before it checks anything, so the weekly PR carries them:

- `uses: actions/checkout@v4` becomes `uses: actions/checkout@<sha> #
  v4.2.2`, naming the full release the floating tag points at today so
  advisories and later updates compare against a real version.
- A mise `node = "22"`, `python = "latest"`, or `.tool-versions` `ruby 3`
  becomes the exact version `mise latest` resolves it to.

- `FROM python:3.13-slim`, `COPY --from=ghcr.io/astral-sh/uv:0.5.4`, and
  a compose `image: postgres:16.4` gain the digest their tag points at:
  `python:3.13-slim@sha256:…`. The tag stays for humans and for updates;
  the digest is what actually gets pulled. Stages, `scratch`, `${VAR}`
  images, and images with no tag are left alone.

Once an image carries a digest, every `update` re-resolves it after
patching. A tag the run just moved gets the digest that matches it, and a
tag rebuilt upstream under the same name, which is how a base image ships
its OS security patches, gets the new digest under *Pinned floating
references*. Digests come straight from the registry's API, so docker need
not be installed.

A branch ref (`@main`) has no release to name and is reported for you to
pin by hand. `pin --check`, or `--fail-on-unpinned` on `check` and
`update`, exits 8 while any floating reference remains.

A repo with neither a `.doneram.pkl` nor a `Dockerfile` runs on discovery
alone. With a config, discovered pins join the declared ones, and any pin a
`tools` entry already matches is never checked twice. Set `discover =
false` to turn discovery off, or list tools to skip by their report name:

```pkl
ignore = new { "actions/checkout"; "node" }
```

## Config

A repo with pins that discovery cannot find declares them in `.doneram.pkl`. Each
tool names a resolver and the sites where its version appears as a literal.
doneram patches every site together, so a version pinned in three files
cannot end up disagreeing with itself.

```pkl
afterPatch = "./sync_with_ctt.sh"

tools = new {
  ["golangci-lint"] = new {
    sites = new {
      new Site {
        file = "go_template/.config/mise/conf.d/template.toml.jinja"
        pattern = #""golangci-lint" = "([\d.]+)""#
      }
      new Site {
        file = "go_template/.github/workflows/ci.yml.jinja"
        pattern = #"version: v([\d.]+)"#
      }
    }
  }
}
```

A pattern carries exactly one capture group, around the version. A site that
sets `expect` declares an exact match count, and any other count fails the
run, because a regex that quietly stops matching is how a pin goes stale
without anyone noticing. A site that leaves `expect` unset takes one or more
matches and patches all of them, which is what a file a project generates
from a template and then extends needs. Either way zero matches fails, and
doneram rescans with a loosened pattern to print ranked candidates.

A site whose version sits on a different line than the text that identifies
it, a pre-commit hook's `rev:` under its `repo:` URL, sets `window` to the
number of consecutive lines the pattern matches against at once. The
pattern must anchor on text unique to that window, or an occurrence can be
counted twice across overlapping windows.

A tool's constraint defaults to `#.#.#`, taking any release without a
suffix. A pin that only ever ships with one, a prerelease tracked on
purpose because upstream hasn't cut a stable release yet, sets `pattern` to
override it: `4.0.0-alpha.*` keeps taking newer alphas of that exact
version and ignores everything else, the same wildcard syntax the
Dockerfile directive uses. A `hold`'s ceiling narrows whichever pattern is
in effect, custom or default.

`afterPatch` runs after a successful patch. In a template repo it
regenerates the rendered output, so a pin and its snapshots never diverge.

`../my_go_template/.doneram.pkl` is the worked example.

## Resolvers

`mise` answers for any tool in its registry, which covers most CLI tooling
and all three of its backends. `mise registry <tool>` reports the upstream
ref when doneram needs to resolve natively instead, and `mise ls-remote`
gives the full version list a pattern can filter.

The rest resolve directly: Docker Hub, GHCR, npm, PyPI, Cargo, Composer,
RubyGems, apk, apt, yum, GitHub releases, GitHub branch HEAD, and CDNJS.

A command resolver covers what no registry query can answer. It runs a
command and parses name, current, and latest out of its output, which is how
a resolved dependency graph (`uv tree --outdated`, `npm outdated`,
`cargo outdated`) becomes one more source of drift in the same report. Give
the tool sites as well and the command answers what the newest version is
while the sites say where to write it, so a `pyproject.toml` constraint
moves with what the resolver actually says is installable. An entry the
command reports without a newer version is up to date, so make the latest
group optional in the pattern.

### Cloud-published versions

Some pins only a cloud provider can answer: the newest AMI, the GKE
versions a release channel offers. doneram asks the provider's own CLI
(`aws`, `gcloud`) with whatever credentials, profile, and region it already
has, and only reads. Where the CLI is missing or has no credentials, the
site is reported as skipped and the run still passes, so the same config
works on a laptop with a login and in CI without one.

| Resolver | `resolverName` | Answers with |
| --- | --- | --- |
| `aws-ssm` | an SSM parameter, e.g. `/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64`, optionally `@<region>` | the parameter's value |
| `aws-ami` | `<owner>/<name pattern>`, e.g. `amazon/al2023-ami-2023.*-x86_64`, optionally `@<region>` | the newest available AMI ID by creation date |
| `gke` | `<location>/<CHANNEL>`, e.g. `us-central1/REGULAR`, or just `<location>`, optionally `@<project>` | the newest version the channel (or location) offers that matches the pattern |

```pkl
["base-ami"] {
  resolver = "aws-ssm"
  resolverName = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64@us-east-1"
  sites { new { file = "infra/main.tf"; pattern = #"ami\s*=\s*"(ami-[0-9a-f]+)""# } }
}
```

AWS publishes the newest Amazon Linux, Bottlerocket, Windows, and EKS
optimized AMIs as public SSM parameters under `/aws/service/`, so
`aws-ssm` tracks what AWS itself calls latest; `aws-ami` covers an image no
parameter tracks, such as a vendor's or your own Packer build (owner
`self`). An AMI ID is per region, so name the region the pin is for.

Private images in ECR (`<account>.dkr.ecr.<region>.amazonaws.com`) and
Artifact Registry (`*-docker.pkg.dev`, `gcr.io`) are pinned to digests like
any other image, logging in with `aws ecr get-login-password` or `gcloud
auth print-access-token`. A public Artifact Registry image needs no login.

### Pins that track a branch

A version compares as a version. A commit SHA does not, so a pin tracking a
branch reports how far behind it is in commits and in time, and how old the
pinned commit itself is. When the upstream repo has tags newer than the pin,
doneram says so, because tracking a tag beats tracking a moving branch.

## CLI

```bash
# Check the repo config plus discovered pins (Dockerfile directives included)
doneram check

doneram check --config path/to/.doneram.pkl
doneram check --only jq --only hk
doneram check --format json
doneram check --fail-on-drift
doneram check --fail-on-vuln
doneram check --snapshot snapshot.json
doneram check --min-age 0

# Patch in place, relock, and run the config's validate command
doneram update
```

`--only` narrows a run to named tools and fails on a name the config does
not declare, so a typo cannot quietly check nothing. `--format json` writes
the summary to stdout and nothing else, which is what a script should read.

`check` never writes. Drift is reported in the output and the JSON summary.
`--fail-on-drift` makes it an exit code too, for a CI job that should go red
rather than open a pull request.

A pin keeps the precision it was written with: `version: v2.12` moves to
`v2.15`, not `v2.15.0`, because a floating minor was chosen on purpose.

### Major action bumps

A major version is not risky in itself, so doneram judges each major action
bump against how the repo actually uses the action. It reads the action's
`action.yml` at both tags and compares it with each step's `with:` keys and
every `steps.<id>.outputs.*` the workflow reads:

- **breaking**: an input the step sets was removed, an input became
  required and the step does not set it, or an output the workflow reads
  was removed.
- **review**: a default the step relies on changed, or an input it sets was
  deprecated.
- **safe**: nothing the step uses changed. Runtime moves (`node20` →
  `node24`) and lines the release notes flag as breaking are still listed
  for context.

Breaking and review bumps get their own *Needs review* section in the pull
request, so routine updates stay a quick read.

Drift is the only soft outcome. A site that fails to resolve, matches the
wrong number of times, or fails to patch exits non-zero,
because a pin nobody checked looks exactly like a pin that is current.

The exit code says which kind of problem a run hit, so a workflow can retry
a flaky registry but stop on a broken config. When several apply, the first
in this table wins.

| Code | Meaning |
| --- | --- |
| 0 | Every site checked; nothing gated failed |
| 2 | Config could not be loaded, or `--only` names an unknown tool |
| 5 | A file could not be patched |
| 3 | A pattern matched a different number of times than it expects |
| 4 | A resolver could not answer (registry or network), worth a retry |
| 7 | `--fail-on-vuln` and a pin with a known advisory is still on disk |
| 8 | `--fail-on-unpinned` (or `pin --check`) and a floating reference remains |
| 6 | `--fail-on-drift` and a pin is out of date |

`--fail-on-vuln` is the CVE gate: run `update --fail-on-vuln` and a
vulnerable pin that doneram could fix is patched and passes, while one held
below its fix by a ceiling fails the job.


Library majors (PyPI, npm, cargo) are graded the same three ways from
their own release notes: doneram finds the source repository in the
registry's metadata, reads the GitHub release of each major crossed (or the
CHANGELOG section for it), and picks out the lines announcing a breaking
change. A backticked name in one of those lines that this repo's code
uses makes it *breaking*, with the name cited; breaking lines that touch
nothing here make it *review*; notes that announce nothing breaking make
it *safe*. Unreadable notes are *review*, never safe.

## Update policy

Three controls decide whether a newer version is offered, each settable per
tool with a global default.

The **constraint** is the version pattern above, and a hold narrows it
further with a ceiling and a reason.

The **minimum release age** keeps doneram from proposing a version that went
public minutes ago, which is the cheap defense against a compromised or
immediately-yanked release. `--min-age` defaults to 72h, following
Dependabot's own three-day cooldown. Set it to 0 to take releases as they
land, or raise it where a bad version would be expensive.

The cooldown applies where the registry publishes a release date: GitHub
Releases, npm, and PyPI. A project that only cuts tags has no date to read,
so its pins are offered as soon as the tag appears, and so are mise tools,
whose versions come from `mise ls-remote` without dates. When the newest
release outside the window is older than the pin itself, the pin is kept and
reported as behind, never patched backwards.

A **security fix waives the cooldown**. When a pin has a published advisory
and the normal update is held by `--min-age`, or does not reach the fix,
doneram moves the pin to the minimum release that clears every advisory, and
the pull request lists it under *Security fixes*, marked *ahead of
--min-age*. Only the smallest fixing release is taken early; the rest of the
cooldown still applies to everything newer.

**Yanked versions** are checked both ways, on PyPI and crates.io (yanked)
and npm (deprecated). doneram never proposes one, and reports a pinned
version that has since been withdrawn, with the registry's reason, because
that one is already installed everywhere. A withdrawn pin is the one case
doneram moves backwards, to the newest release still standing.

## End of life

A tool that sets `eol` to an [endoflife.date](https://endoflife.date)
product (`nodejs`, `python`, `amazon-aurora-postgresql`) gets the end-of-life
date of the cycle its current pin belongs to, in the report and under `eol`
in the JSON summary with `daysLeft`. The cycle is the longest one the pin
equals or starts with at a dot, so `3.13.5` is `3.13` and `17.11` is `17`. A
pin no cycle covers fails the run. doneram reports the date and leaves the
threshold to whoever reads the summary.

With no config at all, doneram also finds what has an end of life on its
own and looks each one up:

| Where | What | Product |
| --- | --- | --- |
| `mise.toml`, `.tool-versions` | python, node, go, ruby, java, terraform, deno, bun, … | matching runtime |
| Dockerfile `FROM`, compose `image:` | `python:3.13-slim`, `node:22`, `postgres:16`, `debian:bookworm`, `alpine:3.20`, … | cycle read from the tag (or codename) |
| workflow `runs-on` and matrix | `ubuntu-22.04`, `macos-13`, `windows-2019` | `github-actions-runner-images` |
| `requires-python`, `engines.node`, `go.mod` `go` | the oldest version the project still claims to support | python, nodejs, go |
| quoted model IDs in code and config | `"claude-3-opus-20240229"`, `"gpt-4-0613"` | `claude`, `openai-api-models` |
| Terraform, CloudFormation/SAM, Serverless | Lambda `runtime`, RDS/Aurora `engine_version`, EKS `version`, ElastiCache and OpenSearch `engine_version` | `aws-lambda`, `amazon-rds-*`, `amazon-aurora-*`, `amazon-eks`, `amazon-elasticache-redis`, `amazon-opensearch` |
| `X-GitHub-Api-Version` in code | `2022-11-28` | GitHub's own `/versions` list: a version no longer listed is past its end |

Anything past its end of life, or within 90 days of it, is reported and
listed under *End of life* in the PR body. doneram never changes these:
moving a runtime or a model is a migration to plan, not a version bump.
`--fail-on-eol` exits 9 while anything is past its date. Test files and
fixtures are not scanned for model IDs, and a reference no cycle covers is
skipped quietly.

### Inferred from release history

Most pins (an action, a CLI, a library) have no endoflife.date entry. For
those, doneram reads the project's own release history (GitHub releases,
npm, PyPI, and for a mise tool, wherever its backend points: `aqua:`,
`github:`, and github.com Go modules to GitHub, `pipx:` to PyPI, `npm:` to
npm) and judges the version the run leaves on disk:

- **Support window.** For every release line (a major, or a 0.x minor), how
  long patches kept coming after the next line shipped. A line that has gone
  longer than that without a release, while a newer line exists, has most
  likely ended.
- **Abandonment.** No release at all for four times the project's usual gap
  between releases, and never less than a year.

A verdict is *confident* when it has at least three past lines to learn the
habit from (or eight releases, for abandonment) and the silence is twice
the longest window the history shows. A confident verdict is reported as
past end of life and counts toward `--fail-on-eol`; a weaker one is listed
as likely unmaintained and never fails a run. A pin this run moves to a
maintained line is judged at its new version, so it is not reported.

## Vulnerabilities

Advisories come from [OSV](https://osv.dev), which covers PyPI, npm, Go,
crates, and the distro ecosystems (`Debian:12`, `Alpine:v3.19`) that a
container's package list lands in. For what is inside a base image, doneram
shells out to trivy or grype, which read the image layers statically rather
than running the container.

A vulnerable pin reports two candidates, labeled: the minimum patched
version, and the latest version matching the pin's own pattern. If the
normal update clears the advisories it is taken as usual; otherwise the
minimum patched version is, ahead of the cooldown (see Update policy).
SHA-pinned actions are covered through OSV's GitHub Actions ecosystem, and a
fix for one is resolved back to its commit SHA before it is written.

A hold is never overridden. When the only fix sits above the ceiling, the
report says held, vulnerable, and no fix underneath it, and leaves the call
to you.

### Lockfiles

The pins above are the ones a repo writes by hand. Everything else, the
transitive graph, lives in lockfiles, and doneram checks those too with no
config: every package in `uv.lock`, `poetry.lock`, `pdm.lock`,
`Cargo.lock`, `package-lock.json`, `pnpm-lock.yaml`, `go.mod`,
`Gemfile.lock`, and `composer.lock` under the repo goes to OSV in one batch.
A vulnerable package is reported with the minimum release that clears all
of its advisories, and `update` raises it by handing the lockfile back to
its own package manager (`uv lock --upgrade-package urllib3==1.26.5`,
`cargo update --package time --precise 0.2.23`, `go get
golang.org/x/net@v0.23.0`, ...). doneram never resolves a graph itself; a
package manager that is not installed leaves the command in the report to
run by hand. Findings count toward `--fail-on-vuln`, and `lockfiles =
false` turns the scan off.

### Declared ranges

A lockfile moves inside the ranges a manifest declares, and the floors of
those ranges rot: `requests>=2.20` still claims to work with a release
nobody has tested against in years. `update` keeps them in step for every
project that pairs a manifest with its lock: `pyproject.toml` with
`uv.lock`, `package.json` with `package-lock.json` or `pnpm-lock.yaml`,
and `go.mod`.

1. The package manager upgrades the lock within the declared ranges (`uv
   lock --upgrade`, `npm update --package-lock-only`, `pnpm update
   --lockfile-only`, `go get -u ./...` then `go mod tidy`). When a Go
   module needs a newer toolchain, `go get go@X` raises the `go` directive
   and the upgrade is retried once.
2. Each declared floor is raised to the version now locked: `>=X` in
   `[project] dependencies`, `[project.optional-dependencies]`, and
   `[dependency-groups]`, and `^X`, `~X`, `>=X`, or an exact `X` in
   `dependencies`, `devDependencies`, and `optionalDependencies`. Files are
   edited in place, so extras, markers, other clauses, comments, and
   formatting stay as written, and a floor is never lowered. Tags, URLs,
   `workspace:`, `file:`, `npm:` aliases, and compound ranges are left
   alone. Go needs no such step, since a `require` is already a floor.
3. The lock is relocked against the raised floors (`uv lock`, `npm install
   --package-lock-only --ignore-scripts`, `pnpm install --lockfile-only
   --ignore-scripts`).

These ride in the `updates` PR under *Raised declared floors*, and in
every `validate` trial. A project whose package manager is not installed
is skipped with the command to run.

A new major outside a declared range (a `<3` cap, or a `^`/`~` spec) is
never taken silently. `check` and `update` report it as a review bump of the
range itself, `requests` `>=2.31,<3` to `>=3.1.0,<4` or `lodash` `^4.17.21`
to `^5.0.0`, and the plan gives it a `review-pypi-requests` or
`review-npm-lodash` PR that rewrites the spec and relocks. `manifests =
false` turns all of this off.

### Provenance

For npm and PyPI pins, doneram compares how the proposed release was
published with how the current one was: npm's SLSA provenance in the
registry and PyPI's PEP 740 attestations both name the source repository
and workflow that built a release. A release that lost the attestation its
predecessor had, was built from a different repository, or (npm, with no
attestation either side) was pushed by a different account goes to a
`review-<tool>` PR of its own with the evidence, even when it fixes an
advisory: a rushed "fix" from a hijacked account is exactly what this
catches.

### Dependabot alerts for every pin

Dependabot reads language manifests and nothing else, so it never sees a
SHA-pinned action, a tool in mise.toml, or an image tag. `--snapshot
<path>` writes those pins as a [dependency submission
snapshot](https://docs.github.com/en/rest/dependency-graph/dependency-submission),
and once it is posted, GitHub raises Dependabot alerts for them like any
other dependency. Pins map to purls by resolver: `pypi`, `npm`, `cargo`,
`gem`, `composer`, `docker`, `github`, `githubactions`, and `golang` for a
mise `go:` tool; a pin with no purl type is left out. The snapshot records
what is on disk, including sites whose registry failed to answer.
`.github/workflows/dependency-graph.yml` submits one on every push to main.

## GitHub Action

doneram writes a JSON summary; the workflow turns it into pull requests.
A plan job runs `update` on a throwaway checkout and splits what changed
into groups, each of which becomes its own PR:

- `updates`: security fixes, routine updates, safe majors, lockfile fixes,
  raised declared floors, pinned references, and refreshed digests
- `review-<tool>`: one per major that changes something the repo uses, or
  falls outside a declared range
- `failing-<tool>`: one per bump that breaks `validate` (below), with the
  failure in the body, to be fixed on its branch

A risky bump then never blocks the rest from merging. The plan lists the
groups in `$GITHUB_OUTPUT` as `groups`, and `update --plan <summary> --group
<id>` applies just one of them.

```yaml
name: Freshness

"on":
  schedule:
    - cron: "0 9 * * 1"
  workflow_dispatch: {}

permissions:
  contents: read

jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      groups: ${{ steps.plan.outputs.groups }}
      plan: ${{ steps.encode.outputs.plan }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: kyleking/doneram@c5120304a06b6c524a9b96dd6408b01e8b453e65 # v0.5.1
        with:
          version: v0.5.1
      - id: plan
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: doneram update --output /tmp/plan.json
      - id: encode
        run: echo "plan=$(base64 -w0 /tmp/plan.json)" >> "$GITHUB_OUTPUT"

  pr:
    needs: plan
    if: needs.plan.outputs.groups != '[]'
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    strategy:
      fail-fast: false
      matrix:
        group: ${{ fromJSON(needs.plan.outputs.groups) }}
    env:
      HAS_APP: ${{ secrets.DONERAM_APP_CLIENT_ID != '' }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: kyleking/doneram@c5120304a06b6c524a9b96dd6408b01e8b453e65 # v0.5.1
        with:
          version: v0.5.1
      - id: check
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          PLAN: ${{ needs.plan.outputs.plan }}
          GROUP: ${{ matrix.group }}
        run: |
          echo "$PLAN" | base64 -d > /tmp/plan.json
          doneram update --plan /tmp/plan.json --group "$GROUP" --output /tmp/doneram.json

      # A GITHUB_TOKEN push cannot touch workflow files, so the app token is
      # used when the repo has the secrets and the default token otherwise.
      - id: app-token
        if: env.HAS_APP == 'true' && steps.check.outputs.has_upgrades == 'true'
        uses: actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1 # v3.2.0
        with:
          client-id: ${{ secrets.DONERAM_APP_CLIENT_ID }}
          private-key: ${{ secrets.DONERAM_APP_PRIVATE_KEY }}

      - uses: peter-evans/create-pull-request@5f6978faf089d4d20b00c7766989d076bb2fc7f1 # v8.1.1
        if: steps.check.outputs.has_upgrades == 'true'
        with:
          token: ${{ steps.app-token.outputs.token || secrets.GITHUB_TOKEN }}
          branch: doneram/${{ matrix.group }}
          draft: true
          delete-branch: true
          title: ${{ steps.check.outputs.title }}
          body: ${{ steps.check.outputs.body }}
```

Each group has a stable branch, so each has at most one open PR, recreated
from scratch on the next run after it merges or closes. Resolution runs
again in each PR job, so a release published between the two jobs can show
up in one PR and not the plan; the next week's run settles it.

## Fleet

`doneram fleet` runs doneram over the local checkouts in one directory:
every direct child whose name matches `--match` (and not `--exclude`) and
that is a git repo. Each repo runs what `check` runs, with its own
`.doneram.pkl` or on what discovery finds, in the normal environment.

```bash
doneram fleet                                   # every repo under .
doneram fleet --dir ~/src --match '^acme-' --exclude legacy
doneram fleet --report fleet.md --json fleet.json
doneram fleet --dir ~/src --apply               # commit updates on doneram/updates
```

By default nothing is changed: it prints one report with a row per repo
(updates, vulnerable pins and lockfile packages, floating references, and
end-of-life items), then each repo's drift, findings, and errors.
`--report` and `--json` also write it to files. `--workers` (default 4)
sets how many repos run at once.

`--apply` runs `update` in each repo and commits the result as
`chore: doneram updates`, listing what changed, on the `doneram/updates`
branch, recreated from the current HEAD each run. It works on a detached
HEAD and checks the original branch out again afterwards, so the branch
you are on and your working tree are untouched. A repo with uncommitted
changes, or with `doneram/updates` checked out, is skipped. Nothing is
pushed and no PR is opened; the report gives each repo's commit.

The commit runs the repo's own hooks; a hook that rejects it fails that
repo, and its branch and tree are restored. `--min-age` works as it does
for `check`.

The exit is non-zero (4) only when a repo failed outright.

## Validation

`validate` in `.doneram.pkl` is a command (`"go test ./..."`) the plan run
executes after applying every update. When it fails, doneram bisects: it
replays subsets of the bumps from a git snapshot of the tree, finds each
bump the failure needs (up to three, in about log2(n) runs each), and moves
each to a `failing-<tool>` PR carrying the failing output, while the rest
ship in `updates`. If it fails with no bump applied at all, nothing is
split and the updates PR says so. Lockfile fixes, declared-floor syncs,
pinned references, and `afterPatch` run in every trial, so a bump is only blamed for what it
changes.

The command is whatever proves the repo still works: a test suite, a config
parse, a formatter, a generated project's own CI. For a base image bump
that is a build, `validate = "docker build ."`, which catches a tag that
no longer builds.

Validation is optional per tool. A bumped CDN URL or Action SHA has nothing
to build, and doneram reports those as updated but unvalidated rather than
pretending otherwise.

## Finishing a bump with an agent

doneram stops where a deterministic tool has to: a `failing-<tool>` PR whose
bump breaks validation, or a `review-<tool>` PR for a major or a changed
build provenance. `doneram explain <tool>` prints what finishing one needs,
on that PR's branch: the version change against the base branch, every
place it is pinned, the release notes' breaking changes that name something
this repo uses, any provenance warning, and (with `--validate`) the
validate output. It only reads, and it works for a person as well as an
agent.

Two agent skills drive the rest, in the `.agents/skills` layout that
coding agents share (Claude Code reads them through the `.claude/skills`
symlink):

- [`doneram-fix-bump`](.agents/skills/doneram-fix-bump/SKILL.md) runs
  `explain`, adapts the code to the new version, and loops on `validate`
  until it passes. It stops instead of editing on a provenance warning, and
  never skips a test to get green.
- [`copier-update-conflicts`](.agents/skills/copier-update-conflicts/SKILL.md)
  resolves the `.rej` files and conflict markers a template update leaves,
  and lists local changes worth backporting to the template.

Copy `.agents/skills/` into a repo (or a template, so every generated
project gets it) to use them there.

## License

MIT
