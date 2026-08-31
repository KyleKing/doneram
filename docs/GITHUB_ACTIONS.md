# Using Doneram in GitHub Actions

## Installation

Install a pinned release binary onto `PATH` with the composite action:

```yaml
name: Check Dockerfile Updates
on:
  schedule:
    - cron: '0 10 * * 1'  # Weekly on Monday
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: kyleking/doneram@v1

      - name: Check for updates
        run: doneram check -f Dockerfile --verbose
```

## Multi-file Checking

Check multiple Dockerfiles:

```yaml
- name: Check all Dockerfiles
  run: doneram check -f "docker/*/Dockerfile" -f "**/Dockerfile" --workers 4
```

## Auto-update Workflow

Automatically create PRs for updates:

```yaml
name: Auto-update Dockerfiles
on:
  schedule:
    - cron: '0 10 * * 1'
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: kyleking/doneram@v1

      - name: Update Dockerfiles
        run: doneram update -f Dockerfile --format json > updates.json

      - name: Create Pull Request
        if: hashFiles('updates.json') != ''
        uses: peter-evans/create-pull-request@v5
        with:
          commit-message: "chore(docker): update dependencies via Doneram"
          title: "Update Docker dependencies"
          body: |
            Automated Dockerfile updates from Doneram.

            See updates.json for details.
          branch: doneram/updates
          labels: dependencies,docker
```

## Output Formats

### Standard Output
```bash
doneram check -f Dockerfile --format stdout
```

### JSON (for automation)
```bash
doneram check -f Dockerfile --format json
```

### GitHub Actions Format
```bash
doneram check -f Dockerfile --format github-actions
```

## Advanced Configuration

### Verbose Output
```bash
doneram check -f Dockerfile --verbose
```

Enables debug logging showing:
- Request start/end with URLs
- Retry attempts with delays
- Version matching details
- Package resolution steps

### Parallel Processing
```bash
doneram check -f "docker/*/Dockerfile" --workers 8
```

Process multiple Dockerfiles concurrently.

## Integration Examples

### Matrix Strategy

Check different Dockerfiles in parallel jobs:

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        dockerfile:
          - Dockerfile
          - docker/app/Dockerfile
          - docker/worker/Dockerfile
    steps:
      - uses: actions/checkout@v4
      - name: Install Doneram
        run: |
          # ... installation steps ...
      - name: Check ${{ matrix.dockerfile }}
        run: doneram check -f ${{ matrix.dockerfile }} --verbose
```

### Conditional Updates

Only update if specific conditions are met:

```yaml
- name: Check for updates
  id: check
  run: |
    doneram check -f Dockerfile --format json > check.json
    UPDATES=$(jq '.updates | length' check.json)
    echo "count=$UPDATES" >> $GITHUB_OUTPUT

- name: Update if needed
  if: steps.check.outputs.count > 0
  run: doneram update -f Dockerfile
```

## Supported Package Managers

Doneram supports checking versions for the following package managers in `FROM` and `COPY --from` instructions:

- **Container Registries**: Docker Hub, GitHub Container Registry (GHCR)
- **Python**: PyPI (`pip install`)
- **JavaScript**: npm (`npm install`)
- **Rust**: crates.io (`cargo install`)
- **Ruby**: RubyGems (`gem install`)
- **PHP**: Packagist (`composer require`)
- **Alpine**: APK (`apk add`)
- **Debian/Ubuntu**: APT (`apt-get install`)
- **RHEL/Fedora**: yum (`yum install`)

## Troubleshooting

### Rate Limiting

If you encounter rate limits:

1. The retry logic will automatically back off with exponential delay
2. Check the `Retry-After` header in logs when using `--verbose`
3. Consider spacing out scheduled runs
4. For high-volume usage, implement caching of API responses

### Logging Levels

- **INFO** (default): Check/update progress, resolved versions
- **DEBUG** (`--verbose`): Detailed resolution steps, HTTP requests
- **WARN**: Rate limits, retries
- **ERROR**: Unrecoverable errors

Example verbose output:
```
time=2026-01-17T10:00:00.000Z level=INFO msg="checking file" file=Dockerfile format=dockerfile
time=2026-01-17T10:00:00.100Z level=DEBUG msg="resolving package" resolver=pypi package=requests
time=2026-01-17T10:00:01.200Z level=INFO msg="resolved package" resolver=pypi package=requests version=2.31.0
time=2026-01-17T10:00:01.300Z level=INFO msg="check completed" file=Dockerfile updates_found=3
```
