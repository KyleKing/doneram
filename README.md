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

### Homebrew (coming soon)

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

## Config

A repo with pins outside Dockerfiles declares them in `.doneram.pkl`. Each
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

A pattern carries exactly one capture group, around the version. Each site
declares how many times it expects to match, defaulting to once. A count
that disagrees fails the run, because a regex that quietly stops matching is
how a pin goes stale without anyone noticing. On a failed match doneram
rescans with a loosened pattern and prints ranked candidates.

A site whose version sits on a different line than the text that identifies
it, a pre-commit hook's `rev:` under its `repo:` URL, sets `window` to the
number of consecutive lines the pattern matches against at once. The
pattern must anchor on text unique to that window, or an occurrence can be
counted twice across overlapping windows.

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

A command resolver covers what no regex can reach. It runs a command and
parses name, current, and latest out of its output, which is how a whole
dependency graph (`uv tree --outdated`, `npm outdated`, `cargo outdated`)
becomes one more source of drift in the same report.

### Pins that track a branch

A version compares as a version. A commit SHA does not, so a pin tracking a
branch reports how far behind it is in commits and in time, and how old the
pinned commit itself is. When the upstream repo has tags newer than the pin,
doneram says so, because tracking a tag beats tracking a moving branch.

## CLI

```bash
# Check the repo config, or ./Dockerfile if there is none
doneram check

doneram check -f docker/api/Dockerfile
doneram check --format json
doneram check --fail-on-drift

# Patch in place
doneram update -f Dockerfile
doneram update --skip-build
```

`check` never writes. Drift is reported in the output and the JSON summary.
`--fail-on-drift` makes it an exit code too, for a CI job that should go red
rather than open a pull request.

## Update policy

Three controls decide whether a newer version is offered, each settable per
tool with a global default.

The **constraint** is the version pattern above, and a hold narrows it
further with a ceiling and a reason.

The **minimum release age** keeps doneram from proposing a version that went
public minutes ago, which is the cheap defense against a compromised or
immediately-yanked release. The default is 24 hours. Set it to 0 to take
releases as they land, or raise it where a bad version would be expensive.

**Yanked versions** are checked both ways. doneram never proposes one, and
reports a currently-pinned version that has since been yanked, because that
one is already installed everywhere.

## Vulnerabilities

Advisories come from [OSV](https://osv.dev), which covers PyPI, npm, Go,
crates, and the distro ecosystems (`Debian:12`, `Alpine:v3.19`) that a
container's package list lands in. For what is inside a base image, doneram
shells out to trivy or grype, which read the image layers statically rather
than running the container.

A vulnerable pin reports two candidates, labeled: the minimum patched
version, and the latest version matching the pin's own pattern. A CVE fix
waives the minimum release age, and the report says so rather than taking a
rushed security release quietly.

A hold is never overridden. When the only fix sits above the ceiling, the
report says held, vulnerable, and no fix underneath it, and leaves the call
to you.

## GitHub Action

doneram writes a JSON summary; the workflow turns it into a pull request.

```yaml
name: Freshness
on:
  schedule:
    - cron: "0 9 * * 1"
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false

      - uses: kyleking/doneram@v1

      - id: check
        run: doneram update --output /tmp/doneram.json

      - uses: peter-evans/create-pull-request@v8
        if: fromJSON(steps.check.outputs.has_upgrades)
        with:
          branch: chore/doneram-updates
          draft: true
          delete-branch: true
          title: ${{ steps.check.outputs.title }}
          body: ${{ steps.check.outputs.body }}
```

One stable branch means one open freshness PR at a time, recreated from
scratch on the next run after it merges or closes.

## Validation

An update can declare a command that must pass before it is kept. For a
Docker pin that is a build plus the image's HEALTHCHECK, which catches a
tag that no longer builds. Elsewhere it is whatever proves the file still
works: a config parse, a formatter, a generated project's own CI.

Validation is optional per tool. A bumped CDN URL or Action SHA has nothing
to build, and doneram reports those as updated but unvalidated rather than
pretending otherwise.

## License

MIT
