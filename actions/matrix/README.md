# Compatibility matrix action

Checks the contracts repository already checked out by the caller. Validates its
published manifests, renders Markdown, JSON, and HTML reports, and fails on
incompatible deployed contracts. Warnings pass. Reports are uploaded as the
`compatibility-matrix` artifact even when the compatibility gate fails.

```yaml
permissions:
  contents: read
jobs:
  matrix:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: Wirefit/wirefit/actions/matrix@<published-commit>
        with:
          version: <published-commit>
```

Pin the action and `version` to the same published WireFit commit. `version` is a
Git ref (default `master`); the action builds the CLI from that source using the
Go version in its `go.mod`. Optional `contracts-path` defaults to `.`, and
`stale-days` defaults to `30`.

Keep push, pull request, schedule, and manual triggers in the caller's workflow.
For a private store, check it out with a token granting Contents read permission.
The separate `actions/pages` action publishes the matrix website.
