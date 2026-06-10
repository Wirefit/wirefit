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
extractors/java/test.sh        # extractor round-trip (needs JDK 11+)
examples/demo.sh               # full end-to-end acceptance scenario
```

## Ground rules

- Every diff-rule change needs a corpus case for both directions (P→C and C→P).
- Determinism is non-negotiable (NF3): identical inputs must produce byte-identical IR
  and identically ordered findings. CI re-runs extraction twice to enforce it.
- The IR keyword set is frozen per `internal/ir` — additions require a SPEC §7 amendment
  in the same PR.
- Extractor behavior must mirror what the serializer actually does. When Jackson and the
  spec disagree, Jackson wins and the spec gets fixed.

## Conduct

Be kind, assume good faith. Disagreements about compatibility semantics are the point of
this project — argue with fixtures, not adjectives.
