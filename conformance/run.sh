#!/usr/bin/env bash
# Cross-language conformance harness (PRD 2.7): every extractor must produce
# HASH-IDENTICAL IR for each case under conformance/cases/<Name>/. The
# committed corpus (internal/confexpected/expected/) is the arbiter: ts and
# java run through the public protocol via `wirefit extractor-test`, exactly
# like any third-party extractor; go cases run through `wirefit extract`
# (the built-in gotool).
set -euo pipefail
cd "$(dirname "$0")/.."

./extractors/java/fetch-jars.sh
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
CP="$JARS/jackson-core-2.22.0.jar:$JARS/jackson-databind-2.22.0.jar:$JARS/jackson-annotations-2.22.jar:$JARS/jackson-datatype-jdk8-2.22.0.jar:$JARS/jakarta.annotation-api-3.0.0.jar"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

go build -o "$WORK/wirefit-java" ./cmd/wirefit-java
go build -o "$WORK/wirefit-ts" ./cmd/wirefit-ts

# The corpus's "service build": wirefit-java extracts from compiled classes.
javac --release 17 -cp "$CP" -d "$WORK/classes" conformance/cases/*/*.java
JAVA_CMD=("$WORK/wirefit-java" --classpath "$WORK/classes:$CP")

# --update-expected regenerates the committed corpus from the java extractor
# (the reference implementation), through the same protocol.
if [ "${1:-}" = "--update-expected" ]; then
  python3 - "${JAVA_CMD[@]}" <<'EOF'
import json, os, subprocess, sys
cases = sorted(os.listdir("conformance/cases"))
req = {"schemaVersion": 1, "projectDir": ".",
       "specs": [{"ref": f"conformance.{c}", "role": "provided"} for c in cases]}
p = subprocess.run(sys.argv[1:], input=json.dumps(req), capture_output=True, text=True, check=True)
resp = json.loads(p.stdout)
if resp.get("error"):
    sys.exit(f"wirefit-java: {resp['error']}")
for c in cases:
    with open(f"internal/confexpected/expected/{c}.ir.json", "w") as f:
        json.dump(resp["schemas"][f"conformance.{c}"], f)
print(f"updated expected IR for {len(cases)} cases")
EOF
fi

# Build wirefit AFTER a possible corpus update: extractor-test compares
# against the expected IR embedded in this binary.
go build -o "$WORK/wirefit" ./cmd/wirefit

{
  echo "cases:"
  for dir in conformance/cases/*/; do
    name="$(basename "$dir")"
    echo "  - {name: $name, spec: conformance.$name}"
  done
} > "$WORK/java-cases.yaml"
{
  echo "cases:"
  for dir in conformance/cases/*/; do
    name="$(basename "$dir")"
    [ -f "$dir/$name.ts" ] || continue
    echo "  - {name: $name, spec: \"conformance/cases/$name/$name.ts#$name\"}"
  done
} > "$WORK/ts-cases.yaml"

fail=0
echo "== java (protocol) =="
"$WORK/wirefit" extractor-test --cases "$WORK/java-cases.yaml" --project . -- "${JAVA_CMD[@]}" || fail=1
echo "== ts (protocol) =="
"$WORK/wirefit" extractor-test --cases "$WORK/ts-cases.yaml" --project . -- "$WORK/wirefit-ts" || fail=1

# Go column (PRD 3.1): cases without a .go fixture are documented N/A
# (Go has no enums or idiomatic tagged unions).
echo "== go (built-in) =="
for dir in conformance/cases/*/; do
  name="$(basename "$dir")"
  gofile="$(ls "$dir"/*.go 2>/dev/null | head -1 || true)"
  [ -n "$gofile" ] || continue
  lower="$(echo "$name" | tr '[:upper:]' '[:lower:]')"
  {
    echo "service: conformance-go"
    echo "schema-version: 1"
    echo "consumes:"
    echo "  - id: conformance.$lower"
    echo "    provider: javaside"
    echo "    dto: ./cases/$name#$name"
  } > "$WORK/go-$name.yaml"
  "$WORK/wirefit" extract -f "$WORK/go-$name.yaml" --project conformance --ir "$WORK/go-ir-$name" >/dev/null
  ghash="$("$WORK/wirefit" hash "$WORK/go-ir-$name/consumes/javaside/conformance.$lower.ir.json")"
  ehash="$("$WORK/wirefit" hash "internal/confexpected/expected/$name.ir.json")"
  if [ "$ghash" = "$ehash" ]; then
    echo "OK   $name  $ghash"
  else
    echo "FAIL $name: go $ghash, expected $ehash"
    fail=1
  fi
done

[ "$fail" = 0 ] && echo "conformance: all cases hash-identical across extractors"
exit $fail
