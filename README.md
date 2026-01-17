# Doner

Automated Dockerfile version updater that keeps your container images and dependencies fresh, tested, and production-ready.

## Installation

### Homebrew (coming soon)

```bash
brew install kyleking/tap/doner
```

### From source

```bash
go install github.com/kyleking/doner/cmd/doner@latest
```

### Binary releases

Download from [GitHub Releases](https://github.com/kyleking/doner/releases).

## Usage

### CLI

```bash
# Check for updates (dry-run)
doner check

# Check specific Dockerfile
doner check -f workers/Dockerfile

# Verbose output
doner check -v
```

### GitHub Action

```yaml
name: Check Dockerfile Updates
on:
  schedule:
    - cron: '0 10 * * 1'
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: kyleking/doner@v1
        with:
          command: check
          file: Dockerfile
```

## Comment Directive Syntax

Add directives above Dockerfile instructions to control version updates:

```dockerfile
# doner: python:3.13.#-alpine*
FROM python:3.13.11-alpine3.21

# doner: uv:0.#.#
COPY --from=ghcr.io/astral-sh/uv:0.9.24 /uv /uvx /bin/

# doner: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache bash=5.2.15-r0 curl=8.5.0-r0 git
```

### Version Patterns

- `3.13.#` - Pin major.minor, allow patch updates
- `3.#.#` - Pin major, allow minor+patch
- `#.#.#` - Allow any version
- `*` - Wildcard for suffixes (alpine3.21 -> alpine3.22)
- `ignore` - Skip version checking

## License

MIT
