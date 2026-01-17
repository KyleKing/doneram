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
# Check for updates (dry-run, looks for ./Dockerfile by default)
doner check

# Check specific Dockerfile
doner check -f docker/api/Dockerfile

# Test with example fixtures
doner check -f test/fixtures/simple-python.Dockerfile
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
          file: docker/api/Dockerfile
```

## Comment Directive Syntax

Add directives above Dockerfile instructions to control version updates:

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

### Version Patterns

- `3.13.#` - Pin major.minor, allow patch updates
- `3.#.#` - Pin major, allow minor+patch
- `#.#.#` - Allow any version
- `*` - Wildcard for suffixes (alpine3.21 -> alpine3.22)
- `ignore` - Skip version checking

## License

MIT
