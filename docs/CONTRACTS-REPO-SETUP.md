# Setting up a contracts repo on GitHub

End-to-end recipe for standing up a real contracts repo with the Pages matrix site and
wiring service repos to it. Everything here was verified against live repos; the concept
itself is in `USER-GUIDE.md` ("Contracts repo": a plain git repository, no broker, no
service to run).

The short version: almost everything travels in committed workflow files. Exactly two
things are repo *settings*, and both are lost if a repo is deleted and recreated:
Pages enablement on the contracts repo, and the token secret on each service repo.

## 1. The contracts repo

An empty git repository is a valid contracts repo. The first `wirefit publish` creates
the layout; do not scaffold anything.

### Pages matrix workflow

Commit `.github/workflows/pages.yml`:

```yaml
name: matrix pages

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  pages: write
  id-token: write
  contents: read     # an explicit permissions block drops the default read; checkout needs it

# publishes arrive as bursts of pushes; only the latest matters
concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  matrix-pages:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
    steps:
      - uses: Wirefit/wirefit/actions/pages@master
        with:
          contracts-repo: OWNER/contracts
          token: ${{ github.token }}
```

Notes:
- The workflow runs inside the contracts repo, so the built-in `github.token` is enough.
  No PAT, no secret. (The snippet in `actions/pages/action.yml` shows
  `secrets.CONTRACTS_REPO_TOKEN`; that is only needed when the workflow lives in a
  *different* repo than the contracts repo.)
- The `github-pages` environment is created automatically on the first deploy.

### Enable Pages (repo setting, the part people miss)

The pages action does not enable Pages for you; without this it fails with
"Get Pages site failed":

```bash
gh api repos/OWNER/contracts/pages -X POST -f build_type=workflow
```

UI equivalent: Settings → Pages → Source: "GitHub Actions". This is the one setting on
the contracts repo, and it must be redone if the repo is ever recreated.

## 2. Service repos

Commit `.github/workflows/contracts.yml` per the snippet in `actions/check/action.yml`:

```yaml
on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read
  pull-requests: write   # sticky PR comment
  issues: write

jobs:
  contracts:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: Wirefit/wirefit/actions/check@master
        with:
          contracts-repo: OWNER/contracts
          token: ${{ secrets.CONTRACTS_REPO_TOKEN }}
```

PR → `check` + sticky comment. Push to main → `check` + `publish`. With several services
in one repo, use one job per manifest and chain them with `needs:` (provider first) so
publishes never push to the contracts repo concurrently.

### The token (repo setting)

`github.token` cannot cross repos, and publishing writes to the contracts repo, so each
service repo needs a `CONTRACTS_REPO_TOKEN` Actions secret with read (PRs) / write
(pushes to main) access to the contracts repo.

The working recipe (LAUNCH.md "auth recipe" item): a **fine-grained PAT** scoped to just
the contracts repo with `Contents: read and write`, stored as the secret:

```bash
gh secret set CONTRACTS_REPO_TOKEN -R OWNER/service-repo --body "<the PAT>"
```

For quick experiments `--body "$(gh auth token)"` also works, but it carries the full
scope of your gh login and dies with it; do not use it beyond testing. Deploy keys were
not validated for this flow.

## 3. Checklist

| where | in git (survives recreation) | repo setting (redo after recreation) |
|---|---|---|
| contracts repo | `pages.yml` workflow | Pages: build via GitHub Actions |
| each service repo | `contracts.yml` workflow, `contracts.yaml` manifests | `CONTRACTS_REPO_TOKEN` secret |

Actions themselves are enabled by default; no branch protection, environments, or
webhooks are required.

## 4. Current caveats (until fixed/released)

- Reference the actions as `@master`. The documented `@v0` tag does not exist yet, and
  the `actions/` directory is not in v0.1.0/v0.2.0.
- Pass `version: master` to the *pages* action: `wirefit matrix --format html/-o`
  is not in v0.2.0.
- Both actions currently need a pre-step in the calling workflow until the PATH fix
  lands, and the check action needs two more workarounds for monorepos and publish
  identity; see `docs/actions-check-fixes-plan.md` for the fixes and the exact
  workaround snippets.
