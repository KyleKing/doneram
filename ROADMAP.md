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
compromised or immediately-yanked release. A version that fixes a published
CVE overrides the wait, and the report says plainly that it did, so a rushed
security release is never taken silently.

The ecosystem converged on this while doneram was being built. Dependabot
now waits three days by default with no configuration, Renovate has
`minimumReleaseAge`, pnpm 10.16 has `minimumReleaseAge`, and Yarn 4.10 has
`npmMinimalAgeGate`, and all of them exempt security updates. A review of 21
reported supply chain incidents found the malicious versions were pulled
within hours of publication. doneram's default follows Dependabot at three
days rather than the 24 hours planned here, settable to 0 or extended per
tool.

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

## M5.5: Fleet correctness and ergonomics

Wave 1 and wave 2 shipped, and running doneram across 13 repos exposed gaps
no single-repo milestone could surface.

### The schema is copy-pasted, and it has drifted

Every `.doneram.pkl` inlines its own `Site` and `Tool` classes, because
doneram publishes no pkl package. Three variants exist today: six repos
declare `expect: Int(this > 0) = 1`, seven declare `expect: Int(this > 0)?`,
and yak-shears added `pattern: String?` before the others had it. The `= 1`
default makes the "one or more" match count unreachable in the six repos
carrying it, so the fix for count drift is not in effect where it was
written.

Publishing `Config.pkl` as a pkl package and amending it from each repo
turns a schema change into a version bump. It lands before anything else
adds a field.

### The `ecosystem` field cannot be set

`engine.Site.Ecosystem` exists and the loader reads it, but no config
declares it, so M6's OSV lookup only fires where a resolver kind carries a
default mapping. Of 82 resolver declarations across the fleet, 54 are
`github-action`, 13 `github-release`, 10 `mise`, and 2 `npm`. Two pins are
eligible for an advisory query.

### Resolution is serial and repeats itself (done)

45 sites in my_go_template took 10.1s of wall clock at 0.33s of user time,
so it was all waiting. `RunSites` was a plain `for` loop with no
concurrency and no memoization, and those 45 sites cover 26 distinct tools,
so 19 resolutions asked a question already answered (setup-go four times,
hk three). `RunSites` now runs eight sites at a time and routes every
resolve, detail, and command through a single-flight cache, which takes the
same run to 2.25s.

Rate limiting was the other half. The retry transport backed off on a 429
but nothing paced requests, and unauthenticated GitHub is 60 requests an
hour, which is what made the first CI run report 20 unresolved sites and
exit 0. A per-host limiter now caps four requests in flight per host and
reads `x-ratelimit-remaining`; a host that reports its quota spent fails
the rest of the run immediately with the reset time, because an hour-long
window outlasts any backoff a single run can afford.

### `GITHUB_OUTPUT` uses a fixed heredoc delimiter

Resolver-supplied text goes into `body<<DONERAM_EOF`. A version string or
command output containing that line, or a newline inside a title, writes
arbitrary step outputs. The delimiter should be random per run, and a title
containing a newline should be rejected.

### Directives only reach Dockerfiles

`# doneram:` parses inside a Dockerfile and attaches only to `FROM` and
`COPY --from`. It cannot annotate a mise.toml pin, a workflow `uses:` line,
a shell variable default, or a CDN URL, which covers every pin shape the
fleet actually has, so all real usage is pkl config. `hold[reason; <ceiling]`
is reachable only through a directive, which is why yak-shears' htmx ceiling
is a bare `2.#.#` with a TODO comment rather than a hold carrying its reason.

Either the directive front end generalizes to any comment-bearing line, or
it stops being a headline feature and pkl becomes the only interface. What
is not defensible is a syntax documented as general that works on one file
type nobody pins through.

### Smaller edges

`--config`, `--only <tool>`, `--fail-on-drift`, a `--format json` the config
path honors, and an honest root usage string have shipped. What remains:

- `check --apply` and `update` do the same thing on the config path
- One report line per site, so my_go_template prints `setup-go: up to date`
  four times in a row
- Every unresolved site and every count mismatch collapses into one exit
  code, so a workflow cannot tell one kind of breakage from another

Done when the fleet amends a published schema, a full check of
my_go_template finishes in about a second, and every pin in the fleet is
eligible for an advisory lookup.

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

### GitHub Actions coverage, resolved

OSV defines a `GitHub Actions` ecosystem whose package name is
`{owner}/{repo}`, and the advisories are real: `GHSA-mrrh-fwg8-r2c3`, the
March 2025 tj-actions/changed-files compromise, carries a well-formed
`introduced: 0` / `fixed: 46.0.1` range. A versioned `querybatch` for
`tj-actions/changed-files` at `45.0.7` returns nothing, while the same
package queried without a version returns the advisory. OSV has no version
comparator registered for that ecosystem, which matches osv-scanner's own
fix note about avoiding parse errors on unsupported GitHub Actions version
ranges.

So doneram queries action pins by package alone and evaluates the ranges
with `pkg/version`, which it already has for constraint filtering. That
turns 54 of the fleet's 82 pins from ineligible into covered, and it is the
single largest coverage win available.

The pin itself is a SHA with the version in a trailing comment
(`c2a8761… # v4.3.0`), so the advisory query reads the version out of the
comment. A SHA with no comment cannot be checked, which is one more reason
the comment stays required.

### Feeding GitHub's own graph

A second, complementary channel: the [dependency submission
API](https://docs.github.com/en/rest/dependency-graph/dependency-submission)
takes a snapshot of purl-identified dependencies against a commit, and
submitted dependencies receive Dependabot alerts and security updates for
any ecosystem the GitHub Advisory Database covers. doneram already knows
every pin, its resolver, and its version, which is exactly a snapshot.

The appeal is that alerting, deduplication, and dismissal then live in the
GitHub UI beside every other alert, rather than in a PR body doneram
renders. The catch is purl coverage: purl-spec defines `github`, `golang`,
`pypi`, `npm`, `docker`, and `oci`, and defines no GitHub Actions type at
all. Whether GitHub's graph accepts a non-standard `pkg:githubactions/...`
is untested and has to be verified against a real repository before this is
promised.

### SBOM

Two shapes, and doneram should not confuse them.

For a base image, doneram shells out rather than reading layers itself, per
the mise, uv, and pkl precedent. syft plus grype splits generation from
scanning, so the SBOM is an artifact other tools can consume; trivy does
both in one binary and also emits SPDX, CycloneDX, and GitHub's
dependency-snapshot format directly. Both produce SPDX 2.3 and CycloneDX
1.6. trivy's snapshot output is the cheapest path to the submission API
above, and grype's curated database is the quieter one for alerting. The
scanner is detected, not required, which is what `vulnscan.Detect` already
does.

For doneram's own pins, an SBOM is a rendering of the config plus resolved
versions, and needs no scanner at all. Emitting CycloneDX from a
`.doneram.pkl` gives a repo an inventory of everything Dependabot cannot
see, which is the same data the submission API wants. Worth doing only if
something consumes it.

### Open questions

- Terraform and Pulumi have no OSV ecosystem at all
- Whether GitHub's dependency graph accepts an action pin as a purl
- Whether to cache advisory results, given a weekly run against a batch
  endpoint is already cheap

Done when a Dockerfile pin with a known CVE in its base image reports the
advisory, its severity, and both candidate versions, and a held pin with no
fix under its ceiling is visible as such.

## Deferred

- Native PR creation. The JSON contract plus an action works. Revisit when
  the workflow boilerplate is visibly repeated across repos.
- A fleet view. 13 repos each run their own workflow and open their own PR,
  with no way to ask what is stale everywhere without 13 checkouts. Mostly
  falls out of `--config` plus a report mode.
- Release notes in the PR body. doneram knows the resolver and both
  versions, so a compare link or a release-page link costs nothing and is
  what a reviewer actually wants.
- An SBOM of a repo's own pins, if anything consumes it.
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
