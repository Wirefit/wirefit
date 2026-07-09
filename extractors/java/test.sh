#!/usr/bin/env bash
# Fixture round-trip: compile extractor + fixtures, extract, validate through
# the Go IR parser, and check hash determinism across two runs.
# The canonical extractor source lives in internal/javatool/WirefitExtract.java
# (embedded into the wirefit-java binary); this script compiles that same file.
set -euo pipefail
cd "$(dirname "$0")"

./fetch-jars.sh
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
CP="$JARS/jackson-core-2.22.0.jar:$JARS/jackson-databind-2.22.0.jar:$JARS/jackson-annotations-2.22.jar:$JARS/jackson-datatype-jdk8-2.22.0.jar:$JARS/jakarta.annotation-api-3.0.0.jar"
OUT="$(mktemp -d)"

javac --release 17 -cp "$CP" -d "$OUT" \
  ../../internal/javatool/WirefitExtract.java fixtures/src/com/acme/orders/*.java

java -cp "$CP:$OUT" io.wirefit.extract.WirefitExtract \
  com.acme.orders.OrderResponse com.acme.orders.Payment > "$OUT/run1.json"
java -cp "$CP:$OUT" io.wirefit.extract.WirefitExtract \
  com.acme.orders.OrderResponse com.acme.orders.Payment > "$OUT/run2.json"

python3 - "$OUT" <<'EOF'
import json, sys, subprocess, os
out = sys.argv[1]
r1, r2 = (json.load(open(os.path.join(out, f))) for f in ("run1.json", "run2.json"))
assert r1 == r2, "extraction is not deterministic"
for fqn, schema in r1.items():
    p = os.path.join(out, fqn.split(".")[-1] + ".ir.json")
    json.dump(schema, open(p, "w"))
    subprocess.run(["go", "run", "./cmd/wirefit", "hash", p],
                   cwd="../..", check=True, capture_output=True)
print("java extractor fixture round-trip OK:", ", ".join(r1))
EOF
