# GitHub App setup for Freshness

The freshness workflows run without an app: `create-github-app-token` runs
only when `DONERAM_APP_CLIENT_ID` is set, and `create-pull-request` falls back
to `GITHUB_TOKEN` otherwise. The fallback cannot push a branch that edits
`.github/workflows/**`, so a run whose only updates are action pins fails at
the push rather than silently succeeding — the app is what lifts that limit.

## Why an app at all

`GITHUB_TOKEN` cannot push a branch that edits `.github/workflows/**`, and
doneram pins actions inside workflow files wherever it runs. A scoped
installation token can, and it expires an hour after the job mints it.

## Setup

**1. Create the app** (or reuse an existing one). Under
[Developer settings → GitHub Apps](https://github.com/settings/apps), "New
GitHub App". A name, any homepage URL, and webhook disabled are enough. Give
it three repository permissions, all read and write: **Contents**, **Pull
requests**, and **Workflows**.

**2. Generate the private key** on the app's key-pairs page
(`https://github.com/settings/apps/<app-name>/key_pairs`). The browser
downloads a `.pem` to `~/Downloads`. That file is the credential: whoever
holds it can push to every repo the app is installed on. It is shown once and
cannot be re-downloaded.

**3. Note the Client ID** on the app's page. The workflows pass it to
`create-github-app-token`'s `client-id` input; the App ID is never used.

**4. Install it, scoped.** "Install App" in the left sidebar, **Only select
repositories**, picking the repos that run freshness. Not "All repositories":
the app can rewrite workflow files, so the blast radius should be the repos
that actually run doneram, and adding one later takes ten seconds. If the
install prompts to accept permissions, accept them.

**5. Store the secrets** — `DONERAM_APP_CLIENT_ID` and
`DONERAM_APP_PRIVATE_KEY` on each installed repo:

```sh
OWNER=<your-github-user-or-org>
CLIENT_ID=<the client id>
KEY=~/Downloads/<app-name>.<date>.private-key.pem

REPOS=(
  repo-one
  repo-two
)

for r in "${REPOS[@]}"; do
  gh secret set DONERAM_APP_CLIENT_ID --repo "$OWNER/$r" --body "$CLIENT_ID"
  gh secret set DONERAM_APP_PRIVATE_KEY --repo "$OWNER/$r" < "$KEY"
done
```

To build `REPOS` from the repos that already have the workflow:

```sh
REPOS=()
for r in $(gh repo list "$OWNER" --limit 400 --json name --jq '.[].name'); do
  gh api "repos/$OWNER/$r/contents/.github/workflows/freshness.yml" --silent \
    && REPOS+=("$r")
done
```

**6. Get the key out of Downloads.** Store it in a password manager — e.g.
`op document create "$KEY" --title 'doneram private key'` for 1Password —
then delete the file (`rm -P "$KEY"`). GitHub keeps only the public half, so
that copy is the only one if a repo ever needs re-adding.

**7. Verify** on one repo rather than waiting for the next scheduled run:

```sh
gh workflow run freshness.yml --repo "$OWNER/repo-one"
gh run watch --repo "$OWNER/repo-one"
```

## If it fails

A "not installed" error from the app-token step means the install in step 4
missed that repo. A push rejected over the `workflow` scope means the
Workflows permission was left at "No access", and changing a permission
afterward requires accepting the new permissions on the installation before
it takes effect.
