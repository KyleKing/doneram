# Roadmap

Where doneram is going and what has to be true at each step. The reasoning
behind these choices lives in [generalizing.md](./generalizing.md); this file
carries the plan and its exit criteria.

## What doneram replaces, and what it does not

Five hand-written scripts check pinned versions across the repos today, all
built on one copied library:

| File | Repo | Fate |
| --- | --- | --- |
| `scripts/freshness/checkers.py` | yak-shears, my_go_template, calcipy_template | Deleted, three copies |
| `scripts/check_freshness.py` | yak-shears | Deleted |
| `scripts/check_freshness.py` | my_go_template | Deleted |
| `scripts/check_freshness.py` | calcipy_template | Deleted |
| `scripts/check_cdnjs_updates.py` | calcipy_template | Deleted |

Every capability they have maps onto doneram: GitHub release and
commit-by-branch resolution, npm, CDNJS, regex extract and patch, multi-file
grouped patching, hold semantics, version comparison, and report rendering.
The one shape no locator can express is `uv tree --outdated`, which walks a
whole dependency graph rather than reading a pin out of a file. That becomes
a command resolver: doneram runs a command and parses name, current, and
latest out of its output. `npm outdated`, `cargo outdated`, and
`go list -m -u all` are the same shape.

The `freshen` skill is a different thing and survives. It syncs repos with
origin, runs local gates, triages CI on the default branch, resolves
Dependabot security alerts, and rolls template releases out to copier
children. doneram removes one job from it (finding stale pins) and feeds it
structured drift data. It does not replace the judgment work.

## Decisions this plan assumes

- Resolvers shell out to `mise` where the tool is in its registry, natively
  otherwise. `mise registry <tool>` gives the backend and upstream ref for
  the handoff, `mise ls-remote` gives the full version list a constraint can
  filter.
- Config is pkl, evaluated by shelling out to `pkl eval -f json`. pkl-go
  bindings if the subprocess cost ever matters.
- Comment directives compile into locators. One execution engine, two front
  ends.
- doneram emits a JSON summary; a workflow action opens the PR.
- A hold carries a reason and a ceiling: `hold[breaking changes; <3.0.0]`
  keeps taking 2.x updates and refuses to cross 3.0.
- `--fail-on-drift` is opt-in, so both existing CI conventions work.
- Dependabot alerts are reported as urgent, never acted on.
- Build validation stops being the headline and becomes one validation
  command among several. Managing versions is the value.

## M1: Locator engine

The regex-capture-and-patch primitive, independent of Dockerfile syntax.

- A `locator` package: glob, pattern with one capture group, expected match
  count, patch-in-place
- Match count that disagrees with `expect` fails the run and prints ranked
  candidates from a loosened rescan
- pkl config loading via `pkl eval -f json`, with a published `Config.pkl`
  schema module
- `internal/parser` compiles `# doneram:` comments into locators rather than
  resolving them itself

Done when `doneram check` reads `../my_go_template/.doneram.pkl`, reports all
13 tools across 17 sites, and the existing Dockerfile tests still pass
unchanged.

## M2: Resolvers

- mise, shelling out, covering every tool in its registry
- GitHub release, with the prerelease-tag filtering `checkers.py` does
- GitHub commit-by-branch, reporting commits and time behind, the pinned
  commit's own date, and a warning when the repo has tags newer than the pin
- CDNJS, with a warning when it disagrees with the library's GitHub releases
- Command resolver: run a command, parse name/current/latest by regex
- Hold with reason and ceiling, applied through the existing `VersionPattern`

Done when every pin in wave 1 resolves, and doneram's report agrees with the
Python scripts run side by side.

## M3: Wave 1 migration

my_go_template, yak-shears, and wavez. Chosen together because the template
and its child have to agree, and yak-shears carries the pin shapes neither Go
repo has (bash variable default, branch-HEAD SHA with no upstream tags, HTML
CDN marker, hold comment, uv dependency tree).

- Patching, `--apply`, and the JSON summary contract
- `afterPatch` command, so patching my_go_template regenerates `.ctt`
- Scheduled workflow in each repo, opening a standing draft PR
- wavez's mise pins move off `latest` (11 of 13 tools say `latest` today,
  because it was generated before my_go_template pinned them)

Done when `scripts/freshness/` and `check_freshness.py` are deleted from
yak-shears and my_go_template, and a scheduled run has opened a real PR in
each of the three.

## M4: Release

calcipy_template cannot ship a copied script into generated projects and
should not ship a `go install` either.

- Homebrew tap, GitHub release binaries, a pinned composite action
- Version validation and constraints documented
- doneram checks its own pins with its own config

Done when a generated project can run doneram from a pinned action without
building from source.

## M5: Wave 2 migration

calcipy_template and tail-jsonl, held back until wave 1 proves the schema,
because calcipy_template reproduces whatever it does into every project
generated from it.

- calcipy_template's `.jinja` action pins, grouped so one action patches
  across every file it appears in
- CDNJS pins in `mkdocs.yml.jinja`
- The existing `freshness-check.yml` workflow rewired to doneram's JSON,
  keeping `peter-evans/create-pull-request`
- The doneram config itself templated into generated projects

Done when `check_cdnjs_updates.py`, `check_freshness.py`, and
`scripts/freshness/` are gone from calcipy_template, and a project generated
from it gets freshness checks from a pinned doneram action.

## Deferred

- Native PR creation. The JSON contract plus an action works. Revisit when
  the workflow boilerplate is visibly repeated across repos.
- pkl-go bindings.
- Generalizing validation commands beyond Docker build and healthcheck.
  Formatters and syntax checks (`ruff`, `tombi`, `pkl eval`) are the obvious
  next instances, since a bumped pin that breaks a config file should fail
  before it reaches a PR.
