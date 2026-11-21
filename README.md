# Doneram

Doneram keeps pinned versions current. It finds a pin, asks the right
registry what the newest matching version is, patches the file, and
optionally runs a command to prove the update didn't break anything.

A pin is any version literal doneram can find with a regex: a Dockerfile
`FROM` tag, a mise tool version, a GitHub Action SHA, a CDN URL, a bash
variable default. Dockerfiles get a comment syntax because a `FROM` line
reads better with a directive above it than with an entry in a config file.
Everything else is declared in the repo's config.

doneram does not resolve dependency graphs, manage lockfiles, or run copier.
[ROADMAP.md](./ROADMAP.md) draws the full in-scope and out-of-scope line and
carries the milestones. The design reasoning is in
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

The directive applies to the next line.

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

### Several packages on one line

```dockerfile
# doneram: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache bash curl git
```

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
- Plain versions under `[tools]` in `mise.toml`, and in `.tool-versions`.
  `latest`, `lts`, and arrays are not pins and are skipped.

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

### Pins that track a branch

A version compares as a version. A commit SHA does not, so a pin tracking a
branch reports how far behind it is in commits and in time, and how old the
pinned commit itself is. When the upstream repo has tags newer than the pin,
doneram says so, because tracking a tag beats tracking a moving branch.

## CLI

```bash
# Check the repo config plus discovered pins, or ./Dockerfile if there is no config
doneram check

doneram check -f docker/api/Dockerfile
doneram check --config path/to/.doneram.pkl
doneram check --only jq --only hk
doneram check --format json
doneram check --fail-on-drift
doneram check --fail-on-vuln
doneram check --snapshot snapshot.json
doneram check --min-age 0

# Patch in place
doneram update -f Dockerfile
doneram update --skip-build
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

The cooldown applies where the registry publishes a release date, which
today means GitHub Releases. A project that only cuts tags has no date to
read, so its pins are offered as soon as the tag appears. When the newest
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

Anything past its end of life, or within 90 days of it, is reported and
listed under *End of life* in the PR body. doneram never changes these:
moving a runtime or a model is a migration to plan, not a version bump.
`--fail-on-eol` exits 9 while anything is past its date. Test files and
fixtures are not scanned for model IDs, and a reference no cycle covers is
skipped quietly.

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
  pinned references, and refreshed digests
- `review-<tool>`: one per major that changes something the repo uses
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

## Validation

`validate` in `.doneram.pkl` is a command (`"go test ./..."`) the plan run
executes after applying every update. When it fails, doneram bisects: it
replays subsets of the bumps from a git snapshot of the tree, finds each
bump the failure needs (up to three, in about log2(n) runs each), and moves
each to a `failing-<tool>` PR carrying the failing output, while the rest
ship in `updates`. If it fails with no bump applied at all, nothing is
split and the updates PR says so. Lockfile fixes, pinned references, and
`afterPatch` run in every trial, so a bump is only blamed for what it
changes.

Separately, an update can declare a command that must pass before it is kept. For a
Docker pin that is a build plus the image's HEALTHCHECK, which catches a
tag that no longer builds. Elsewhere it is whatever proves the file still
works: a config parse, a formatter, a generated project's own CI.

Validation is optional per tool. A bumped CDN URL or Action SHA has nothing
to build, and doneram reports those as updated but unvalidated rather than
pretending otherwise.

## License

MIT
