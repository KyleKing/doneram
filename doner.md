# Doner - Dockerfile Maintainer 🦌

**Doner** (pronounced "donor") is an automated Dockerfile version updater that keeps your container images and dependencies fresh, tested, and production-ready.

> *"Like Santa's reindeer delivering updates, Doner keeps your Dockerfiles flying smoothly through the CI/CD sky."*

## Overview

Doner automatically:
1. Parses Dockerfiles and detects version-pinned dependencies
2. Checks registries for latest versions matching your constraints
3. Updates versions and validates via Docker build + HEALTHCHECK
4. Queries running containers for package manager updates (apk, apt, etc.)
5. Generates comprehensive update reports with changelogs

**Key Differentiator**: Doner validates updates by actually building and running your containers, catching breaking changes before they reach production.

## Comment Directive Syntax

### Basic Syntax

```dockerfile
# doner: <package-name>:<version-pattern>
```

The directive applies to the **next line** in the Dockerfile.

### Version Patterns

Use `#` as a wildcard for version segments and `*` for suffixes:

```dockerfile
# Example from https://docs.astral.sh/uv/guides/integration/aws-lambda/

# doner: uv:0.9.#
FROM ghcr.io/astral-sh/uv:0.9.26 AS uv

# doner: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13 AS builder

# Example from https://github.com/elves/elvish

# doner: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 as builder

# doner: alpine:3.#
FROM alpine:3.19
```

**Pattern Semantics:**
- `3.13.#` - Pin major.minor, allow patch updates (3.13.11 → 3.13.12)
- `3.#.#` - Pin major, allow minor+patch (3.13.11 → 3.14.0)
- `#.#.#` - Allow any version (within major if SemVer)
- `3.13.11` - Fully pinned, no updates
- `*` - Wildcard for suffixes (alpine3.21 → alpine3.22)

### Pre-release Support (Future)

```dockerfile
# doner: package:1.#.#^
# doner: package:1.#.#&
# doner: package:1.#.#!
```

- `^` - Include release candidates (rc, RC)
- `&` - Include beta versions
- `!` - Include alpha versions (highest risk)

### Multi-package Directives

```dockerfile
# doner: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache bash curl git
```

- Comma-separated package:pattern pairs
- `ignore` - Skip version checking for specific package

### Ignoring Updates

```dockerfile
# doner: ignore
FROM legacy-image:1.0.0

# doner: git:ignore, vim:ignore
RUN apk add bash curl git vim  # Only updates bash, curl
```

## CLI Usage

```bash
# Check for updates (dry-run, looks for ./Dockerfile by default)
doner check

# Check specific Dockerfile
doner check -f docker/api/Dockerfile

# Test with example fixtures
doner check -f test/fixtures/elvish.Dockerfile

# Apply updates with build validation
doner update -f Dockerfile

# Apply updates without build validation
doner update -f Dockerfile --skip-build

# Apply updates with build but skip healthcheck
doner update -f Dockerfile --skip-healthcheck

# Output in GitHub Actions format
doner check -f Dockerfile --format github-actions

# Output in JSON format
doner check -f Dockerfile --format json
```

## GitHub Action

```yaml
name: Update Dockerfiles
on:
  schedule:
    - cron: '0 10 * * 1'  # Weekly on Monday
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Doner
        run: |
          curl -L https://github.com/your-org/doner/releases/latest/download/doner-linux-amd64 \
            -o /usr/local/bin/doner
          chmod +x /usr/local/bin/doner

      - name: Check for updates
        id: doner
        run: |
          doner update -f docker/app/Dockerfile --format github-actions
          echo "updates_available=$?" >> $GITHUB_OUTPUT

      - name: Create Pull Request
        if: steps.doner.outputs.updates_available == '0'
        uses: peter-evans/create-pull-request@v5
        with:
          commit-message: "chore(docker): Update dependencies via Doner"
          title: "Update Docker dependencies"
          body-path: doner-summary.md
          branch: doner/updates
          labels: dependencies,docker
```

## Implementation Plan

### Phase 1: Core Foundation (MVP) ✅ COMPLETE

**Goal**: Parse Dockerfiles, update base images, validate with builds

**Status**: All deliverables complete. The `check` and `update` commands are fully functional with Docker build validation and HEALTHCHECK support.

#### Deliverables:
1. ✅ **Project Setup**
   - Go module initialization
   - CLI framework (urfave/cli)
   - Project structure
   - Basic README and documentation

2. ✅ **Dockerfile Parser**
   - Parse Dockerfile instructions (FROM, COPY --from, RUN)
   - Extract doner directives from comments
   - Parse version patterns (wildcards)
   - Line-by-line instruction mapping

3. ✅ **Docker Image Resolver**
   - Docker Hub API client
   - GHCR API client
   - Version comparison/filtering
   - Pattern matching (3.13.#, alpine*)

4. ✅ **Dockerfile Updater**
   - In-memory Dockerfile modification
   - Preserve formatting and comments
   - Write updated Dockerfile to disk

5. ✅ **Build Validator**
   - Docker build execution
   - HEALTHCHECK extraction and execution
   - Error capture and reporting

6. ✅ **Basic Reporter**
   - Console output
   - Simple summary table

#### Success Criteria:
- ✅ Parse valid Dockerfiles with FROM instructions
- ✅ Update `python:3.13.0` to latest 3.13.x
- ✅ Update `golang:1.22-alpine3.19` to latest matching pattern
- ✅ Build Dockerfile and run HEALTHCHECK
- ✅ Output summary of changes

#### Test Cases:
```dockerfile
# Real-world example from elvish project
# doner: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 as builder

RUN apk add --no-cache --virtual build-deps make git

# doner: alpine:3.#
FROM alpine:3.19
```

---

### Phase 2: Package Manager Integration

**Goal**: Query and update inline package versions (pip, npm, apk, apt)

#### Deliverables:
1. **Container Query Engine**
   - Start container from image
   - Execute commands inside container
   - Query apk versions: `apk list --installed`
   - Query apt versions: `dpkg -l`
   - Clean up containers

2. **PyPI Resolver**
   - PyPI JSON API client
   - Version comparison for pip packages
   - Pattern matching for pip install lines
   - Update `pip install package==version`

3. **APK Resolver**
   - Parse apk output from container
   - Query Alpine package database (Repology API)
   - Handle apk version format (5.2.15-r0)
   - Update `apk add package=version`

4. **NPM Resolver**
   - npm registry API client
   - Update `npm install package@version`

5. **Multi-package Directive Support**
   - Parse comma-separated directives
   - Handle `ignore` for specific packages
   - Update only specified packages in RUN commands

6. **Enhanced Reporter**
   - Group by package type
   - Show version changes per resolver
   - Include package manager in summary

#### Success Criteria:
- ✅ Query package versions from running containers
- ✅ Update package versions via apk, apt, pip, npm
- ✅ Handle multi-package directives with exclusions
- ✅ Build, validate, and rebuild after package updates

#### Test Cases:
```dockerfile
# Real-world examples from various projects

# Alpine-based with apk packages
# doner: alpine:3.#
FROM alpine:3.19

# doner: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache bash=5.2.15-r0 curl=8.5.0-r0 git

# Debian-based with apt packages
# doner: debian:bookworm-*
FROM debian:bookworm-slim

# doner: wget:ignore, curl:#.#.#
RUN apt-get update && apt-get install -y wget curl ca-certificates
```

---

### Phase 3: GitHub Actions Integration

**Goal**: Enable seamless GitHub Actions workflow integration

#### Deliverables:
1. **GitHub Actions Output Format**
   - Generate GitHub Actions summary (markdown)
   - Set workflow outputs for downstream jobs
   - JSON format for programmatic consumption
   - STDOUT format (default, human-readable)

2. **CLI Output Options**
   - `--format` flag: stdout, github-actions, json
   - Environment detection (auto-enable in CI)
   - Exit codes for automation

#### Success Criteria:
- ✅ Generate GitHub Actions summary markdown
- ✅ Support multiple output formats
- ✅ Clean integration with GitHub Actions workflows

#### Test Cases:
- GitHub Actions workflow with automatic summary generation
- JSON output for downstream processing
- Multiple Dockerfiles with grouped updates by source

---

### Future Enhancements

**Goal**: Production hardening and ecosystem expansion

#### Potential Features (Prioritized):

**High Priority:**
1. **Error Handling & Resilience**
   - Retry logic with exponential backoff
   - Rate limit handling for registries
   - Graceful degradation on API failures

2. **Testing & CI/CD**
   - Increase test coverage (>80%)
   - Mock registry responses for tests
   - CI/CD pipeline (GitHub Actions)
   - Integration tests with real Dockerfiles

3. **Distribution**
   - GitHub Releases with binaries (goreleaser)
   - Multi-platform builds (Linux, macOS, Windows)
   - Docker image for easy CI/CD integration

**Medium Priority:**
4. **Changelog Fetching**
   - GitHub Releases API (for GHCR images)
   - Docker Hub description parsing
   - PyPI changelog links
   - Include changelogs in output

5. **Advanced Version Patterns**
   - Pre-release support (^, &, !)
   - CalVer support
   - Custom version constraints

6. **Observability**
   - Structured logging
   - Debug mode (`--verbose` flag)
   - Better error messages

**Low Priority:**
7. **Additional Package Managers**
   - Cargo (Rust)
   - Maven/Gradle (Java)
   - Bundler (Ruby)
   - Composer (PHP)

8. **Advanced Features**
   - Parallel processing for multiple Dockerfiles
   - Auto-commit mode with git integration
   - Registry authentication support
   - Custom healthcheck commands

---

## Technical Architecture

### Project Structure

```
doner/
├── cmd/
│   └── doner/
│       └── main.go                 # CLI entrypoint
├── internal/
│   ├── parser/
│   │   ├── dockerfile.go           # Dockerfile AST parsing
│   │   ├── directive.go            # Doner directive parsing
│   │   └── pattern.go              # Version pattern matching
│   ├── resolver/
│   │   ├── resolver.go             # Resolver interface
│   │   ├── docker.go               # Docker Hub/GHCR
│   │   ├── pypi.go                 # PyPI
│   │   ├── npm.go                  # npm registry
│   │   ├── apk.go                  # Alpine APK (via container)
│   │   └── apt.go                  # Debian/Ubuntu APT
│   ├── builder/
│   │   ├── docker.go               # Docker build/run
│   │   ├── healthcheck.go          # HEALTHCHECK execution
│   │   └── query.go                # Container package queries
│   ├── updater/
│   │   ├── updater.go              # Dockerfile modification
│   │   └── writer.go               # File writing
│   ├── reporter/
│   │   ├── reporter.go             # Report generation
│   │   ├── github.go               # GitHub Actions format
│   │   ├── markdown.go             # Markdown format
│   │   └── json.go                 # JSON format
│   ├── config/
│   │   └── config.go               # Configuration parsing
│   └── changelog/
│       ├── fetcher.go              # Changelog fetching
│       ├── github.go               # GitHub releases
│       └── pypi.go                 # PyPI changelogs
├── pkg/
│   ├── version/
│   │   ├── version.go              # Version comparison
│   │   ├── semver.go               # SemVer handling
│   │   └── calver.go               # CalVer handling
│   └── registry/
│       └── client.go               # HTTP client utilities
├── test/
│   ├── fixtures/
│   │   └── dockerfiles/            # Test Dockerfiles
│   ├── integration/                # Integration tests
│   └── mocks/                      # Mock registries
├── docs/
│   ├── USAGE.md
│   ├── CONFIGURATION.md
│   └── CONTRIBUTING.md
├── .goreleaser.yml                 # Release automation
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

### Key Dependencies

```go
// go.mod
module github.com/your-org/doner

go 1.23

require (
    github.com/spf13/cobra v1.8.0          // CLI framework
    github.com/spf13/viper v1.18.2         // Configuration
    github.com/docker/docker v25.0.0       // Docker API
    github.com/moby/buildkit v0.13.0       // Dockerfile parser
    github.com/Masterminds/semver/v3 v3.2.1 // Version comparison
    github.com/rs/zerolog v1.32.0          // Logging
    gopkg.in/yaml.v3 v3.0.1                // YAML parsing
)
```

### Core Data Structures

```go
// Dockerfile representation
type Dockerfile struct {
    Path         string
    Instructions []Instruction
    Directives   map[int]*Directive // Line number → directive
}

type Instruction struct {
    Command  string   // FROM, RUN, COPY, etc.
    Args     []string
    Line     int
    Raw      string   // Original line
}

// Doner directive
type Directive struct {
    Packages []PackageDirective
    Ignore   bool
}

type PackageDirective struct {
    Name    string
    Pattern *VersionPattern
    Ignore  bool
}

type VersionPattern struct {
    Major  string // "#" or specific version
    Minor  string
    Patch  string
    Suffix string // For "-alpine*", etc.
}

// Update result
type Update struct {
    Package     string
    Source      string // "docker", "pypi", "apk", etc.
    OldVersion  string
    NewVersion  string
    Line        int
    Changelog   string // Optional
}

type UpdateResult struct {
    Dockerfile  string
    Success     bool
    Updates     []Update
    BuildError  error
    HealthError error
}
```

### Resolver Interface

```go
type Resolver interface {
    // Name returns the resolver identifier (docker, pypi, apk, etc.)
    Name() string

    // Resolve finds the latest version matching the pattern
    Resolve(ctx context.Context, pkg string, pattern *VersionPattern) (string, error)

    // GetChangelog fetches changelog between versions (optional)
    GetChangelog(ctx context.Context, pkg string, from, to string) (string, error)
}
```

## Example Workflow

### Input Dockerfile

Real-world example from [uv AWS Lambda integration](https://docs.astral.sh/uv/guides/integration/aws-lambda/):

```dockerfile
# doner: uv:0.9.#
FROM ghcr.io/astral-sh/uv:0.9.24 AS uv

# doner: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13 AS builder

ENV UV_COMPILE_BYTECODE=1
ENV UV_NO_INSTALLER_METADATA=1
ENV UV_LINK_MODE=copy

RUN --mount=from=uv,source=/uv,target=/bin/uv \
    --mount=type=cache,target=/root/.cache/uv \
    --mount=type=bind,source=uv.lock,target=uv.lock \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    uv export --frozen --no-emit-workspace --no-dev --no-editable -o requirements.txt && \
    uv pip install -r requirements.txt --target "${LAMBDA_TASK_ROOT}"

# doner: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13

COPY --from=builder ${LAMBDA_TASK_ROOT} ${LAMBDA_TASK_ROOT}
COPY ./app ${LAMBDA_TASK_ROOT}/app

CMD ["app.main.handler"]
```

### Doner Execution

```bash
$ doner check -f test/fixtures/uv-lambda.Dockerfile

Parsed test/fixtures/uv-lambda.Dockerfile:
  Instructions: 12
  Directives:   2

Checking for updates:
  → python:3.13.0 AS builder -> 3.13.11
  → python:3.13.0 -> 3.13.11
```

Future `doner update` would produce:

```dockerfile
# doner: uv:0.9.#
FROM ghcr.io/astral-sh/uv:0.9.30 AS uv

# doner: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13.1 AS builder

ENV UV_COMPILE_BYTECODE=1
ENV UV_NO_INSTALLER_METADATA=1
ENV UV_LINK_MODE=copy

RUN --mount=from=uv,source=/uv,target=/bin/uv \
    --mount=type=cache,target=/root/.cache/uv \
    --mount=type=bind,source=uv.lock,target=uv.lock \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    uv export --frozen --no-emit-workspace --no-dev --no-editable -o requirements.txt && \
    uv pip install -r requirements.txt --target "${LAMBDA_TASK_ROOT}"

# doner: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13.1

COPY --from=builder ${LAMBDA_TASK_ROOT} ${LAMBDA_TASK_ROOT}
COPY ./app ${LAMBDA_TASK_ROOT}/app

CMD ["app.main.handler"]
```

## Success Metrics

### Phase 1:
- Can parse and update 90%+ of real-world Dockerfiles
- Build validation catches breaking changes
- < 2min execution time for single Dockerfile

### Phase 2:
- Supports top 4 package managers (apk, apt, pip, npm)
- Correctly handles multi-package directives
- Updates grouped by package source in output

### Phase 3:
- GitHub Actions summary markdown generation
- Multiple output formats (stdout, json, github-actions)
- Clean CI/CD integration

## Future Considerations

### Reindeer-Themed Naming 🦌

- **Dasher**: Fast parallel processing mode
- **Dancer**: Graceful error recovery
- **Prancer**: Pre-release version support
- **Vixen**: Version pattern matching
- **Comet**: Changelog fetching
- **Cupid**: Dependency coupling detection
- **Donner**: (Thunder) Build validation
- **Blitzen**: (Lightning) Quick mode (skip heavy operations)
- **Rudolph**: Red-flag detection for risky updates

### Extension Points

- Custom resolver plugins
- Pre/post-update webhooks
- Integration with security scanners (Trivy, Grype)
- Dependency graph visualization
- Cost estimation (image size changes)

---

**Let's make Dockerfile maintenance magical!** 🦌✨
