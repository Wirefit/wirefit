#!/usr/bin/env bash
# Cross-language conformance harness (PRD 2.7): for every case under
# conformance/cases/<Name>/, the Java and TypeScript fixtures must produce
# HASH-IDENTICAL IR. This is the proof that the IR abstracts the type system,
# not the language — every later extractor must pass the same corpus.
set -euo pipefail
cd "$(dirname "$0")/.."

./extractors/java/fetch-jars.sh
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
CP="$JARS/jackson-core-2.17.2.jar:$JARS/jackson-databind-2.17.2.jar:$JARS/jackson-annotations-2.17.2.jar:$JARS/jackson-datatype-jdk8-2.17.2.jar:$JARS/jsr305-3.0.2.jar"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

go build -o "$WORK/wirefit" ./cmd/wirefit

# --- Java side: compile all cases, extract each --------------------------
javac --release 11 -cp "$CP" -d "$WORK/classes" \
  internal/javatool/WirefitExtract.java conformance/cases/*/*.java

# --- TS side: one generated manifest covering all cases ------------------
{
  echo "service: conformance-ts"
  echo "schema-version: 1"
  echo "consumes:"
  for dir in conformance/cases/*/; do
    name="$(basename "$dir")"
    lower="$(echo "$name" | tr '[:upper:]' '[:lower:]')"
    echo "  - id: conformance.$lower"
    echo "    provider: javaside"
    echo "    dto: conformance/cases/$name/$name.ts#$name"
  done
} > "$WORK/contracts.yaml"
"$WORK/wirefit" extract -f "$WORK/contracts.yaml" --project . --ir "$WORK/ts-ir"

# --- compare ---------------------------------------------------------------
UPDATE="${1:-}"
mkdir -p internal/confexpected/expected

fail=0
for dir in conformance/cases/*/; do
  name="$(basename "$dir")"
  lower="$(echo "$name" | tr '[:upper:]' '[:lower:]')"
  java -cp "$CP:$WORK/classes" io.wirefit.extract.WirefitExtract "conformance.$name" > "$WORK/$name.java.json"
  python3 -c "
import json, sys
d = json.load(open('$WORK/$name.java.json'))
json.dump(d['conformance.$name'], open('$WORK/$name.java.ir.json', 'w'))
"
  jhash="$("$WORK/wirefit" hash "$WORK/$name.java.ir.json")"
  thash="$("$WORK/wirefit" hash "$WORK/ts-ir/consumes/javaside/conformance.$lower.ir.json")"
  if [ "$UPDATE" = "--update-expected" ]; then
    cp "$WORK/$name.java.ir.json" "internal/confexpected/expected/$name.ir.json"
  fi
  if [ -f "internal/confexpected/expected/$name.ir.json" ]; then
    ehash="$("$WORK/wirefit" hash "internal/confexpected/expected/$name.ir.json")"
    if [ "$ehash" != "$jhash" ]; then
      echo "FAIL $name: drifted from committed expected IR (run with --update-expected only if the change is intentional)"
      fail=1
    fi
  fi
  # Go column (PRD 3.1): cases without a .go fixture are documented N/A
  # (Go has no enums or idiomatic tagged unions).
  gofile="$(ls "$dir"/*.go 2>/dev/null | head -1 || true)"
  if [ -n "$gofile" ]; then
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
    if [ "$ghash" != "$jhash" ]; then
      echo "FAIL $name (go)"
      echo "  java: $jhash"
      echo "  go:   $ghash"
      fail=1
    fi
  fi
  if [ "$jhash" = "$thash" ]; then
    if [ -n "$gofile" ]; then echo "OK   $name  $jhash (java+ts+go)"; else echo "OK   $name  $jhash (java+ts; go: n/a)"; fi
  else
    echo "FAIL $name"
    echo "  java: $jhash"
    echo "  ts:   $thash"
    python3 - "$WORK/$name.java.ir.json" "$WORK/ts-ir/consumes/javaside/conformance.$lower.ir.json" <<'EOF'
import json, sys, difflib
a, b = (json.dumps(json.load(open(f)), indent=1, sort_keys=True).splitlines() for f in sys.argv[1:3])
print('\n'.join(difflib.unified_diff(a, b, 'java', 'ts', lineterm='')))
EOF
    fail=1
  fi
done
[ "$fail" = 0 ] && echo "conformance: all cases hash-identical across extractors"
exit $fail
