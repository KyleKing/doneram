# Generalizing doner into doneram

## Problem

`scripts/freshness/checkers.py` exists, nearly byte-for-byte, in three repos:
`yak-shears`, `my_go_template`, and `calcipy_template`. Each repo pairs it with
its own hand-written wrapper that hardcodes which files to watch and which
pins to patch. `doing.txt` already flagged this: "This repository *could* be
more general (could rename?)".

doner has no released binary yet, so this is the cheap moment to redesign
rather than migrate later.

## What the Python tooling actually does

Four call sites, none of them a Dockerfile:

- `calcipy_template/scripts/check_freshness.py` — scans `.jinja` files for
  `uses: owner/repo@<sha> # <label>` and resolves against GitHub releases or a
  tracked branch HEAD.
- `yak-shears/scripts/check_freshness.py` — three different pin shapes in one
  repo: a bash variable default (`HTMX_VERSION:-1.2.3`), an HTML marker two
  lines above a CDN URL (`<!-- freshness: npm foo -->`), and a trailing
  `# freshness: hold` comment on a `pyproject.toml` line that means "skip
  this one".
- `my_go_template/scripts/check_freshness.py` — a fourth variant, not yet
  read in detail, but the same shape.
- `calcipy_template/scripts/check_cdnjs_updates.py` — CDNJS + GitHub release,
  compared, higher one wins.

The shared `freshness/checkers.py` is a small, project-agnostic library:
fetch GitHub release, fetch GitHub commit by branch, fetch npm latest,
extract a pin by regex, patch a pin in place, compare versions, render a
report. Every wrapper imports it and supplies its own file list and regex.

## Where doner already covers this

`internal/resolver` already has npm, PyPI, Cargo, Composer, RubyGems,
apk/apt/yum, Docker Hub, and GHCR behind one `Resolver` interface. That
interface is a good fit for GitHub release, GitHub commit-by-branch, and
CDNJS as three more implementations — a day of work each, not a redesign.

`internal/reporter` already renders a per-pin report shape close to the
Python `CheckResult`/`render_report` pair.

## Where doner does not

The parser is Dockerfile-only. A directive is a `# doner: pkg:pattern`
comment that applies to the line right after it. Every Python use case above
needs to find a pin *anywhere* in a file, by regex, not on the line after a
marker comment. None of them are Dockerfiles. Some marker conventions place
the pin two lines below the comment, not one. Some have no comment at all —
the pattern lives in the file structure (a bash variable default).

So the real gap is not "add resolvers", it's "the unit of work is not a
Dockerfile line, it's an arbitrary regex capture in an arbitrary file."

## The redesign: locators instead of a Dockerfile parser

Replace "parse a Dockerfile, find FROM lines, look above them for a
directive comment" with a smaller, file-agnostic primitive:

```
Locator:
    glob: str            # which files this applies to
    pattern: str          # regex with one capture group: the current pin
    resolver: str          # which Resolver to ask for the latest value
    constraint: VersionPattern | "ignore"
    apply: "replace-capture" | "replace-pin"   # how patch writes the file back
```

A Dockerfile `FROM` line becomes one locator kind among several — the
existing directive-comment parser is still the right locator for it, since
it needs to associate a comment with the line below. The other cases (HTML
marker, bash variable, trailing hold-comment, bare `uses:` line) are all
"regex capture, resolve, optionally patch in place" and don't need a real
parser at all, mirroring what `extract_pin`/`patch_pin` already do in
Python.

```
current (Dockerfile only):
    directives = parse_dockerfile(file)           # FROM-line-specific
    for d in directives: resolve(d.package, d.pattern)

target (locator-driven):
    locators = load_config(".doneram.pkl")         # one repo, many files
    for loc in locators:
        for file in glob(loc.glob):
            pin = regex_capture(file, loc.pattern)
            latest = resolve(loc.resolver, pin, loc.constraint)
            if latest != pin: patch(file, loc.pattern, latest)
```

This needs a project-level config (`.doneram.pkl` or similar), since these
use cases span a whole repo's worth of files, not one Dockerfile passed with
`-f`. The Dockerfile comment convention can stay as an ergonomic shortcut —
"any `# doner:`-style comment above a line is sugar for a locator" — without
forcing every other case through the same syntax.

## Where this does not generalize cleanly

- **Build validation.** doner's stated differentiator is validating updates
  by actually building the image and running its HEALTHCHECK. None of the
  four Python use cases have an equivalent "does this still work" check — a
  bumped npm CDN pin or a bumped GitHub Action SHA has no build step to run.
  Generalizing means that path becomes optional per-locator, not a universal
  step, which weakens the "we validate, not just report" pitch for the
  non-Docker cases.
- **Container package queries** (`internal/builder/query.go`, apk/apt
  version queries against a running container) have no analog outside
  Docker at all. This code stays Docker-specific regardless of rename.
- **"Hold" semantics differ.** doner's `# doner: ignore` already covers
  yak-shears' `# freshness: hold`, so no new concept is needed there — but
  it's worth confirming the two mean the same thing (skip forever vs. skip
  until manually revisited) before assuming a straight swap.
- **Multi-file pins.** calcipy_template's GitHub Action pins can appear in
  several `.jinja` files at once and all need patching together (see
  `_find_pins` grouping by `(owner_repo, sha, label)`). The locator model
  above patches per-file; it would need to group by resolved value across
  files if a repo has the same pin duplicated, or accept patching each file
  independently as an equally correct outcome.
- **Rename cost is low, not zero.** No released binary, so no broken
  `go install` paths for outside users — but the GitHub repo, module path,
  and Homebrew tap name all move together, and any local shell aliases or
  mise config referencing `doner` need updating by hand.

## mise as a resolver backend

my_go_template's `copier.yml` now defines fifteen tool versions in one
place, substituted by jinja into the mise TOML and the workflows, with two
`PLANNED:` comments pointing here. Those tools span three mise backends
(core/aqua, `go:`, and `pipx:`), so resolving them one upstream at a time
means a name-to-repo table doneram has to maintain by hand.

mise already answers all three questions:

- `mise registry hk` returns `aqua:jdx/hk`, the backend and upstream ref.
  That is the handoff to a native resolver whenever doneram wants stricter
  control than mise offers.
- `mise ls-remote <tool>` returns every published version, not just the
  newest, so the existing `VersionPattern` filter can apply a constraint
  (hold a major, skip prereleases) that `mise latest` cannot express.
- `mise latest <tool>` is the cheap path when no constraint applies.

The registry is a name-to-backend mapping bundled with the mise binary, so
keeping it current means keeping mise current. Version data from
`ls-remote` is fetched live on every call, so nothing about it goes stale.

Shell out to mise where a tool is in the registry, resolve natively where
it is not or where the upgrade rule needs to be stricter than a version
list allows.

## Source of truth and derived files

In a template repo one logical pin exists three times: as
`default: "1.7.12"` in `copier.yml`, as `{{ actionlint_version }}` in the
`.jinja` (no version at all), and as a rendered concrete value in the
`.ctt/` snapshots. Locators target `copier.yml` only. Regenerating the
snapshots is a post-patch step doneram invokes (copier-template-tester),
not something the locator model pattern-matches. A locator must therefore
be able to declare a command that runs after a successful patch.

## Pin shapes to cover

Designing against yak-shears and my_go_template together, the union is:

- bash variable default (`HTMX_VERSION:-1.2.3`)
- branch-HEAD SHA, where upstream publishes no tags at all (codejar on
  `master`)
- HTML marker two lines above a CDN URL
- trailing `# freshness: hold` on a `pyproject.toml` line
- `uses: owner/repo@<sha> # v<tag>`, duplicated across four workflow files
  and patched in all of them together
- mise TOML tool pin in a generated project
- `copier.yml` `default:` under a `*_version` key in the template repo

Of these, branch-HEAD SHA, GitHub release, and CDNJS need resolvers that
do not exist yet.

## Reporting a SHA pin

A version pin compares as a version. A branch-HEAD SHA has no ordering, so
"outdated" has to be measured a different way. Report three things:

- how far behind, in both commits and time (`8 commits, 4 months behind
  master`)
- how old the pinned commit itself is (`pinned commit dated 2025-08-14`),
  which is the number that says whether anyone is maintaining this
- a warning when the upstream repo has tags newer than the pinned commit,
  since tracking a tag is safer than tracking a moving branch

No warning when upstream publishes no tags at all. codejar has none, so
telling you to use one every run is noise you cannot act on.

## Locators that stop matching

A regex that quietly stops matching is how a pin goes stale without anyone
noticing, and it is the failure mode the Python scripts have today:
`extract_pin` returns None and the tool disappears from the report with no
error. Every site declares how many times it expects to match, defaulting
to one. A count that doesn't agree fails the run.

On a failed match, doneram rescans the file with a loosened pattern (any
version-shaped literal near the same key) and prints ranked candidates, so
a moved pin is a one-line config fix rather than a hunt. A site that
legitimately matches several times says so, as hk's `package://` URLs do.

## Why pkl rather than YAML

The config has to express a validation command reused across tools and a
site helper reused across a dozen pins. YAML anchors do that textually, pkl
does it with typed functions and imports, which is how these repos already
configure hk. The cost is a `pkl eval -f json` subprocess, and every repo
here already pins pkl in mise. `../my_go_template/.doneram.pkl` is the
first real config: 13 tools, 17 sites, every pattern verified to match its
expected count against the real files.

## Where this leaves the Python scripts

my_go_template no longer defines tool versions in `copier.yml`. They are
literals in the files that consume them, declared in `.doneram.pkl`, and
nothing updates them until doneram can. Its `check_freshness.py` still
patches GitHub Action pins and golangci-lint, and reports hk without
patching, since hk's version repeats five times and `patch_pin` rewrites
only the first occurrence.

The plan for retiring all of them, with milestones and exit criteria, is in
[ROADMAP.md](./ROADMAP.md).
