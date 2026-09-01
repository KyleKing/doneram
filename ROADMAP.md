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

## Scope

doneram tracks, updates, and validates dependency versions written as
literals in text files. That is the whole job.

**In scope.** Any version literal doneram can find with a regex and resolve
against a registry: Dockerfile `FROM` tags and `apk add pkg=1.2.3` pins,
mise tool versions, GitHub Action `uses:` SHAs, image tags in compose files,
Helm values, and Kubernetes manifests, CDN URLs, and version defaults in
shell scripts, jinja templates, and pkl.

Also in scope, deliberately: the small jobs the package managers do not do.
`uv sync --upgrade` moves `uv.lock` and never touches `pyproject.toml`,
which is [astral-sh/uv#6794](https://github.com/astral-sh/uv/issues/6794),
open, and worked around today by third-party tools
([uv-bump](https://github.com/zundertj/uv-bump),
[uv-upsync](https://github.com/pivoshenko/uv-upsync)). doneram raising a
declared constraint in `pyproject.toml` and then shelling out to uv to
re-lock is a real gap it can fill. The rule is to do as little as possible
and hand the install, resolve, and lock steps back to the tool that owns
them.

**Out of scope.** copier, entirely: template updates, `.rej` resolution, and
`.copier-answers.yml` are somebody else's problem, including reading
`_commit` to compute template drift. Dependency resolution: doneram never
reimplements what uv, go, npm, or mise already do. Container execution for
vulnerability data, since the whole ecosystem reads image layers statically.

Everything the `freshen` skill does beyond pins stays with `freshen`:
syncing repos with origin, running gates, triaging CI, waiting on workflow
runs, commit hygiene, and `.freshen.md` logging. The comparison is only
useful for drawing this line, and past this point the two are unrelated
tools.

**Deferred.** Terraform and Pulumi provider and module constraints. The
version half works with a locator today; the vulnerability half has no OSV
ecosystem, so it needs its own source.

**Needs a new syntax.** `package.json` has no comments, so a repo pinning
through npm or pnpm cannot carry a `# doneram:` directive. Those pins are
declared in config, or in a dedicated key inside the manifest, rather than
inline.

## Update policy

Three orthogonal controls, all per-tool with a global default.

**Constraint.** The version pattern (`3.13.#`) bounds what an update may
move to. A hold adds a ceiling with a reason attached.

**Minimum release age.** A new version must have been public for a set
period before doneram proposes it, which is the cheap defense against a
compromised or immediately-yanked release. Default 24 hours, settable to 0
or extended per tool. A version that fixes a published CVE overrides the
wait, and the report says plainly that it did, so a rushed security release
is never taken silently.

**Yanked versions.** doneram checks yank status both ways: never propose a
yanked version, and flag a currently-pinned version that has since been
yanked, since that one is already installed everywhere.

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
- Vulnerability data comes from OSV and an image scanner, not from
  Dependabot, which cannot see most of these pins at all.
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

## M6: Vulnerability awareness

The reason this milestone exists: Dependabot cannot see most of what doneram
manages. It reads language manifests in a repo. It does not know what is
inside `python:3.13-slim`, what `apk add` installed three layers down, or
which base image tag a compose file names. Those pins can sit vulnerable
indefinitely with nothing complaining.

### Where the data comes from

[OSV.dev](https://osv.dev) answers for every ecosystem doneram already
resolves, verified against the live API:

| Pin | Query | Advisories |
| --- | --- | --- |
| `requests 2.19.0` | PyPI | 10 |
| `lodash 4.17.15` | npm | 6 |
| `golang.org/x/net 0.17.0` | Go | 18 |
| `curl 7.88.1-10` | `Debian:12` | 43 |
| `busybox 1.36.1-r15` | `Alpine:v3.19` | 6 |

Distro ecosystems are first-class, which is the case that matters most here.
Advisories carry CVSS severity and a `fixed` event, so the minimum patched
version is computed rather than looked up. `/v1/querybatch` takes many
queries in one round trip, so a whole Dockerfile costs one request.

OSV cannot go from an image tag to a package list. Nothing can, without
reading the image. Docker Scout and ECR enhanced scanning both do this
statically: Scout uses the image's SBOM attestation when present and indexes
the layers otherwise, then matches against NVD, the GitHub Advisory
Database, and vendor feeds. Neither runs the container. So doneram shells
out to trivy or grype, consistent with the mise, uv, and pkl decisions, and
maps their findings back to the pins it manages.

This is also why `internal/builder/query.go`, which boots a container to ask
apk or apt what is installed, does not get extended here. It answers a
version question, and vulnerability data is read from layers.

### Behavior

- A vulnerable pin reports two candidate versions, labeled: the minimum
  patched version, and the latest version matching the pin's own pattern.
  The choice between a small fix and a normal update stays explicit.
- A pin held below a ceiling whose only fix is above it is reported loudly,
  never overridden. A hold exists because crossing it breaks the build, and
  doneram cannot know whether the CVE is reachable in your code. The report
  says held, vulnerable, and no fix under the ceiling.
- A CVE fix waives the minimum release age, and the report says it did.
- Severity, advisory ID, and a link travel with every finding, so the report
  is actionable without a second lookup.

### Open questions

- GitHub Actions coverage. OSV returned nothing for `actions/checkout` at
  v1, which may mean the ecosystem name is wrong or that action pins need
  the GitHub Advisory Database directly.
- Terraform and Pulumi have no OSV ecosystem at all.
- Whether to cache advisory results, given a scheduled weekly run against a
  batch endpoint is already cheap.

Done when a Dockerfile pin with a known CVE in its base image reports the
advisory, its severity, and both candidate versions, and a held pin with no
fix under its ceiling is visible as such.

## Deferred

- Native PR creation. The JSON contract plus an action works. Revisit when
  the workflow boilerplate is visibly repeated across repos.
- Hold ceiling syntax past exclusive `<`. Whether `<=`, ranges, or several
  clauses are worth supporting is undecided.
- Which files doneram writes for uv. Raising a `pyproject.toml` constraint
  and handing the lock back to uv is agreed; the split between the two is
  not drawn.
- pkl-go bindings.
- Terraform and Pulumi pins, version half and vulnerability half both.
- Generalizing validation commands beyond Docker build and healthcheck.
  Formatters and syntax checks (`ruff`, `tombi`, `pkl eval`) are the obvious
  next instances, since a bumped pin that breaks a config file should fail
  before it reaches a PR.
