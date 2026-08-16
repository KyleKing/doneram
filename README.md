# Doneram

Automated Dockerfile version updater that keeps your container images and dependencies fresh, tested, and production-ready.

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

## Usage

### CLI

```bash
# Check for updates (dry-run, looks for ./Dockerfile by default)
doneram check

# Check specific Dockerfile
doneram check -f docker/api/Dockerfile

# Test with example fixtures
doneram check -f test/fixtures/simple-python.Dockerfile

# Update Dockerfile with latest versions and validate
doneram update -f Dockerfile

# Update without Docker build validation
doneram update -f Dockerfile --skip-build

# Update with build but skip healthcheck
doneram update -f Dockerfile --skip-healthcheck
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
      - uses: kyleking/doneram@v1
        with:
          command: check
          file: docker/api/Dockerfile
```

## Comment Directive Syntax

Add directives above Dockerfile instructions to control version updates:

```dockerfile
# Example from https://docs.astral.sh/uv/guides/integration/aws-lambda/

# doneram: uv:0.9.#
FROM ghcr.io/astral-sh/uv:0.9.26 AS uv

# doneram: python:3.13.#
FROM public.ecr.aws/lambda/python:3.13 AS builder

# Example from https://github.com/elves/elvish

# doneram: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 as builder

# doneram: alpine:3.#
FROM alpine:3.19
```

### Version Patterns

- `3.13.#` - Pin major.minor, allow patch updates
- `3.#.#` - Pin major, allow minor+patch
- `#.#.#` - Allow any version
- `*` - Wildcard for suffixes (alpine3.21 -> alpine3.22)
- `ignore` - Skip version checking

## License

MIT
