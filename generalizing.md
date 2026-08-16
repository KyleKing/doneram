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
    locators = load_config(".doneram.yml")         # one repo, many files
    for loc in locators:
        for file in glob(loc.glob):
            pin = regex_capture(file, loc.pattern)
            latest = resolve(loc.resolver, pin, loc.constraint)
            if latest != pin: patch(file, loc.pattern, latest)
```

This needs a project-level config (`.doneram.yml` or similar), since these
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

## Next steps, in order

1. Design the locator config schema and decide the `.doneram.yml` shape.
2. Extract the regex-capture-and-patch logic Dockerfile parsing currently
   does implicitly into its own package, independent of Dockerfile syntax.
3. Port GitHub-release, GitHub-commit-by-branch, and CDNJS resolvers into
   Go, next to the existing npm resolver.
4. Decide the rename now, before more scaffolding lands, since the module
   path, repo name, and tap name all touch the same string.
5. Migrate yak-shears first, since it's the repo the other two copied their
   `freshness/checkers.py` from. Once its four pin shapes are covered,
   delete `scripts/freshness/` there and replace it with a config plus a
   GitHub Action step. Hold off on calcipy_template until yak-shears proves
   the abstraction, since calcipy_template reproduces this pattern into
   every project generated from it.
