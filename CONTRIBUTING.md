# Contributing

Thanks for your interest! This project is young — the highest-value contributions right now:

1. **Diff-rule fixtures.** The rule corpus in `internal/diff/selfdiff_test.go` is the
   project's real contract. Found a change classification you disagree with? Open a PR adding
   a failing case — that PR is valuable even without a fix attached.
2. **Type-mapping reports.** Java DTO shapes the extractor maps wrongly or rejects:
   file an issue with the minimal DTO and the expected IR.
3. **Docs friction.** Anything that took you more than 5 minutes to set up is a bug (NF1).

## Development

```
go test ./...                  # unit + rule corpus (no Java needed)
extractors/java/test.sh        # extractor round-trip (needs JDK 17+)
```

The full end-to-end consumer/provider demos live in
[wirefit/examples](https://github.com/wirefit/examples) and run against a released `wirefit`.

## Ground rules

- Every diff-rule change needs a corpus case for both directions (P→C and C→P).
- Determinism is non-negotiable (NF3): identical inputs must produce byte-identical IR
  and identically ordered findings. CI re-runs extraction twice to enforce it.
- The IR keyword set is frozen per `internal/ir` — additions require a SPEC §7 amendment
  in the same PR.
- Extractor behavior must mirror what the serializer actually does. When Jackson and the
  spec disagree, Jackson wins and the spec gets fixed.
- Built-in extractors (`internal/gotool`, `internal/javatool`, `internal/tstool`) share
  their subprocess + cache plumbing via `internal/extrun`: run the child process through
  `extrun.Run` and bootstrap the user cache through `extrun.CacheDir`. Don't re-inline
  either, so invocation changes (timeouts, error handling, output limits) land in one place.
  Adding a *third-party* extractor instead? That's the frozen wire protocol: see
  [docs/extractor-protocol.md](docs/extractor-protocol.md), no WireFit source required.

## Conduct

Be kind, assume good faith. Disagreements about compatibility semantics are the point of
this project — argue with fixtures, not adjectives.
