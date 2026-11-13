# Freshness GitHub App setup

The app exists: [doneram](https://github.com/settings/apps/doneram). What is
left is the private key, the installation, and two secrets per repo.

The workflows no longer fail without it: `create-github-app-token` runs only
when `DONERAM_APP_CLIENT_ID` is set, and `create-pull-request` falls back to
`GITHUB_TOKEN` otherwise. The fallback cannot push a branch that edits
`.github/workflows/**`, so a run whose only updates are action pins fails at
the push rather than silently succeeding — the app is what lifts that limit.

## Why an app at all

`GITHUB_TOKEN` cannot push a branch that edits `.github/workflows/**`, and
every one of these repos pins actions inside its own workflow files. A scoped
installation token can, and it expires an hour after the job mints it.

## Repos that need it

Everything running Freshness — the app edits workflow files wherever it runs:

```sh
REPOS="app-template aragonite backup-yak-shears-py calcipy calcipy_template
chartui copier-template-tester corallium db-slice diacea djot-fmt doner
extract_finances gh-lazydispatch gh-repo-dashboard gh-star-search gh-sweep
jj-diff jj-tui mdformat-hugo mdformat-plugin-template mise-postgres-binary
my_go_template my_lua_template recipes second-look tail-cw tail-jsonl tlr
types-for-jinja vcr-tui wavez what-did-ai-do yak-shears"
```

## What is left to do

**1. Generate the private key.** On the app's
[key pairs page](https://github.com/settings/apps/doneram/key_pairs).
The browser downloads a `.pem` to `~/Downloads`. That file is the credential:
whoever holds it can push to every repo the app is installed on. It is shown
once and cannot be re-downloaded.

**2. Note the Client ID.** On the
[app's page](https://github.com/settings/apps/doneram). The workflows pass it to
`create-github-app-token`'s `client-id` input (the deprecated `app-id` input
is gone from the fleet, so the App ID is never used).

**3. Install it, scoped.** "Install App" in the left sidebar, on your own
account, **Only select repositories**, picking the repos above. Not "All
repositories": the app can rewrite workflow files, so the blast radius should
be the repos that actually run doneram, and adding one later takes ten
seconds.

The app already carries the three repository permissions it needs — Contents,
Pull requests, and Workflows, all read and write. If the install step prompts
to accept new permissions, accept them.

**4. Store the secrets.**

```sh
CLIENT_ID=<the client id>
KEY=~/Downloads/doneram.<date>.private-key.pem
for r in $REPOS; do
  gh secret set DONERAM_APP_CLIENT_ID --repo kyleking/$r --body "$CLIENT_ID"
  gh secret set DONERAM_APP_PRIVATE_KEY --repo kyleking/$r < "$KEY"
done
```

**5. Get the key out of Downloads.**
`op document create "$KEY" --title 'doneram private key'`, then
`rm -P "$KEY"`. GitHub keeps only the public half, so 1Password is the only
copy if a repo ever needs re-adding.

**6. Verify** by hand rather than waiting for Monday:

```sh
gh workflow run freshness.yml --repo kyleking/my_go_template
gh run watch --repo kyleking/my_go_template
```

## If it fails

A "not installed" error from the app-token step means the install in step 3
missed that repo. A push rejected over the `workflow` scope means the
Workflows permission was left at "No access", and changing a permission
afterward requires accepting the new permissions on the installation before
it takes effect.
