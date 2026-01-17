# Test Fixtures

Real-world Dockerfile examples for testing doner functionality.

## Examples

### simple-python.Dockerfile
Basic Python application with uv package manager.
- Tests: Python base image version updates
- Source: Common Python Docker pattern

### elvish.Dockerfile
Multi-stage Go build with minimal Alpine runtime.
- Tests: Go builder image updates, Alpine version updates, multi-stage builds
- Source: https://github.com/elves/elvish/blob/26a8bd5c4ee1eb5c0a2d53578d0368de2b8b3274/Dockerfile

### uv-lambda.Dockerfile
AWS Lambda deployment with uv for dependency management.
- Tests: GHCR image updates, AWS Lambda Python runtime updates
- Source: https://docs.astral.sh/uv/guides/integration/aws-lambda/

### jitsi-base.Dockerfile
Jitsi Meet base image with Debian and complex tooling.
- Tests: Debian base image updates, multi-architecture support
- Source: https://github.com/jitsi/docker-jitsi-meet/blob/master/base/Dockerfile

## Usage

Check all fixtures:
```bash
for f in test/fixtures/*.Dockerfile; do
  echo "=== $f ==="
  ./bin/doner check -f "$f"
done
```

Check specific fixture:
```bash
./bin/doner check -f test/fixtures/simple-python.Dockerfile
```
