# Picking this back up

Session context for the doner-to-doneram generalization. Everything below is
either committed, verified with a command you can rerun, or an open question
nobody has answered yet. Written 2026-08-27.

Read [ROADMAP.md](./ROADMAP.md) first for the plan, [README.md](./README.md)
for what the tool does, and [generalizing.md](./generalizing.md) for why.

## Where things stand

Nothing is implemented. Every commit so far is a rename or a document. The Go
code still parses Dockerfiles only, and no locator, pkl loader, or new resolver
exists.

In this repo:

| Commit | What |
| --- | --- |
| `9b9445a` | Original generalization writeup |
| `a249b68` | Rename doner to doneram across the tree |
| `b86a11b` | mise-as-backend decision |
| `9c6d86e` | SHA reporting, match-count failures, config shape |
| `194a822` | ROADMAP.md added, doneram.md rewritten 637 to 197 lines |
| `2f7899a` | Design doc points at the roadmap |
| `7764aae` | Scope section, update policy, M6 vulnerability milestone |

In `../my_go_template`:

| Commit | What |
| --- | --- |
| `a7a07e3` | 13 `*_version` questions removed from copier.yml, pins became literals |
| `4b4b74d` | Config expressed in pkl, `.doneram.yml` deleted |
| `4dac34f` | Stale `.doneram.yml` references corrected to `.doneram.pkl` |

The GitHub repo is renamed and `origin` points at
`git@github.com:KyleKing/doneram.git`. The local directory is still
`~/Developer/kyleking/doner`, and mise or shell aliases naming the old path
were never checked.

## Decisions already made

Do not relitigate these without a reason.

- **Config is pkl**, evaluated by shelling out to `pkl eval -f json`. pkl-go
  bindings only if the subprocess cost becomes real
- **Resolvers shell out to mise** where the tool is in its registry, natively
  otherwise. Same pattern for uv, trivy, and pkl
- **Comment directives compile into locators**, so there is one execution
  engine with two front ends
- **doneram emits JSON, a workflow action opens the PR.** Native PR creation
  deferred
- A **hold** carries a reason and a ceiling. `hold[breaking changes; <3.0.0]`
  keeps taking 2.x and refuses to cross 3.0, and a vulnerability never
  overrides it
- **`--fail-on-drift` is opt-in**, since yak-shears and calcipy_template
  disagree about whether drift should be an exit code
- **Minimum release age defaults to 24 hours**, settable to 0 or longer. A CVE
  fix waives it and the report says so
- **Wave 1** is my_go_template, yak-shears, and wavez. **Wave 2** is
  calcipy_template and tail-jsonl
- **Out of scope** is copier entirely, dependency resolution, and everything
  the `freshen` skill does beyond pins
- **Build validation stops being the headline.** Managing versions is the
  value, and validation becomes one optional command per tool

## Open questions

Nobody has decided these.

1. **Hold ceiling syntax.** `hold[reason; <3.0.0]` is written as exclusive
   (takes 2.9.9, refuses 3.0.0). Whether `<=`, ranges, or multiple clauses are
   supported was never settled
2. **GitHub Actions vulnerability coverage.** OSV returned zero advisories for
   `actions/checkout` at 1.0.0. Either the ecosystem name is wrong or action
   pins need the GitHub Advisory Database directly. Untested which
3. **Terraform and Pulumi** have no OSV ecosystem. Version freshness works with
   a plain locator today. Vulnerability data needs another source. Deferred,
   not solved
4. **package.json has no comments**, so npm and pnpm pins cannot carry a
   `# doneram:` directive. Config entry or a dedicated manifest key, undecided
5. **Advisory caching.** Probably unnecessary given a weekly run against a batch
   endpoint, but never measured
6. **Which files doneram writes for uv.** Raising a `pyproject.toml` constraint
   then handing the lock back to uv is agreed in principle, but the exact
   boundary is not drawn
7. **Multi-file grouped patching semantics.** calcipy_template patches one
   action SHA across several files as a unit. Whether that is a first-class
   grouping or just several sites resolving to the same value was never
   resolved
8. **Docker code disposition.** `internal/builder/query.go` boots a container to
   ask apk or apt what is installed. It stays out of the vulnerability path
   (layers are read statically), but whether it survives at all is open

## Verified facts, and how to recheck them

Each of these was run, not assumed.

**mise resolves every backend.** All 15 tools in my_go_template, including the
`go:` and `pipx:` backends:

```sh
mise latest hk                              # 1.55.0
mise registry hk                            # aqua:jdx/hk
mise ls-remote golangci-lint | tail -3      # full version list, not just latest
mise latest go:github.com/golangci/golines  # 0.15.0
mise latest pipx:commitizen                 # 4.17.0
```

`mise ls-remote` matters because `mise latest` gives one answer and cannot
express a constraint. The registry ships with the mise binary, and version data
is fetched live.

**OSV covers every ecosystem doneram resolves**, distro ecosystems included:

```sh
curl -s -X POST 'https://api.osv.dev/v1/query' \
  -d '{"package":{"name":"busybox","ecosystem":"Alpine:v3.19"},"version":"1.36.1-r15"}' | jq '.vulns | length'
```

| Pin | Ecosystem | Advisories found |
| --- | --- | --- |
| `requests 2.19.0` | PyPI | 10 |
| `lodash 4.17.15` | npm | 6 |
| `golang.org/x/net 0.17.0` | Go | 18 |
| `curl 7.88.1-10` | `Debian:12` | 43 |
| `busybox 1.36.1-r15` | `Alpine:v3.19` | 6 |
| `actions/checkout 1.0.0` | `GitHub Actions` | 0, see open question 2 |

`/v1/querybatch` takes many queries per round trip. Advisories carry CVSS
severity and a `fixed` event, so the minimum patched version is computed rather
than looked up.

**The pkl config validates against the real files.** 13 tools, 17 sites, every
pattern matching its expected count:

```sh
cd ../my_go_template && pkl eval -f json .doneram.pkl > /tmp/dn.json
uv run python -c "
import json,re,sys
from pathlib import Path
c=json.loads(Path('/tmp/dn.json').read_text())
bad=[(t,s['file']) for t,sp in c['tools'].items() for s in sp['sites'] if len(re.findall(s['pattern'],Path(s['file']).read_text()))!=s['expect']]
print('tools',len(c['tools']),'bad',bad)
sys.exit(1 if bad else 0)"
```

**Base image scanning is static, not dynamic.** Docker Scout uses the image's
SBOM attestation when present and indexes layers otherwise. ECR enhanced
scanning is Amazon Inspector doing the same at registry level. Neither runs the
container, which is why shelling out to trivy or grype beats reusing
`internal/builder/query.go`.

**`uv sync --upgrade` never touches pyproject.toml.** Confirmed open issue,
with two third-party tools existing to fill the gap.

## Known-stale and pending

- **wavez** has `latest` for 11 of 13 mise tools, because it was generated
  before my_go_template pinned them. my_go_template argues against `latest` in a
  comment its own child never received. Fixed in M3
- **hk is behind**, 1.53.0 pinned in my_go_template against 1.55.0 available. It is
  report-only in `check_freshness.py` there, because the version repeats five
  times and `patch_pin` rewrites only the first occurrence
- **Two action bumps were reverted** during this work so they would not ride
  along in a refactor commit, and are still pending in my_go_template:
  `astral-sh/setup-uv` 9.0.0 to 10.0.1 (a major) and `jdx/mise-action` 4.2.0 to
  4.2.5
- **The local directory rename** and any mise or shell aliases pointing at
  `~/Developer/kyleking/doner`

## Repos and files that matter

| Path | Why |
| --- | --- |
| `../my_go_template/.doneram.pkl` | The only real config. 13 tools, 17 sites |
| `../my_go_template/scripts/check_freshness.py` | Action SHAs plus golangci-lint, hk report-only |
| `../my_go_template/sync_with_ctt.sh` | The `afterPatch` command |
| `../yak-shears/scripts/check_freshness.py` | Most pin shapes in one repo |
| `../calcipy_template/scripts/check_freshness.py` | Grouped multi-file action pins |
| `../calcipy_template/scripts/check_cdnjs_updates.py` | CDNJS plus GitHub, higher wins |
| `../calcipy_template/.github/workflows/freshness-check.yml` | The only working scheduled run and PR automation |
| `../wavez/.config/mise/conf.d/template.toml` | Generated child stuck on `latest` |
| `~/.claude/skills/freshen/` | The orchestration doneram does not replace |

## The pin shapes to design against

The union across yak-shears and my_go_template, which is what the schema has to
survive:

- bash variable default (`HTMX_VERSION:-1.2.3`)
- branch-HEAD SHA where upstream publishes no tags at all (codejar on `master`)
- HTML marker two lines above a CDN URL
- trailing `# freshness: hold` on a `pyproject.toml` line
- `uses: owner/repo@<sha> # v<tag>` duplicated across four workflow files
- mise TOML tool pin in a generated project
- literal version in a `.jinja` that copier renders into `.ctt/`

## Links

- [OSV API](https://google.github.io/osv.dev/api/)
- [astral-sh/uv#6794](https://github.com/astral-sh/uv/issues/6794), pyproject.toml constraint bumping
- [uv-bump](https://github.com/zundertj/uv-bump) and [uv-upsync](https://github.com/pivoshenko/uv-upsync), the workarounds
- [ECR enhanced scanning](https://docs.aws.amazon.com/AmazonECR/latest/userguide/image-scanning-enhanced.html)
- [Docker Scout SBOMs](https://docs.docker.com/scout/how-tos/view-create-sboms/)
- [Docker Hub vulnerability scanning](https://docs.docker.com/docker-hub/repos/manage/vulnerability-scanning/)
- [jdx/hk](https://github.com/jdx/hk), the pkl config precedent these repos already use

## Where to start

M1 in the roadmap: the locator package, pkl loading, and making the Dockerfile
parser compile into locators instead of resolving directly. Its exit criterion
is `doneram check` reading `../my_go_template/.doneram.pkl` and reporting all 13
tools across 17 sites with the existing Dockerfile tests still green.
