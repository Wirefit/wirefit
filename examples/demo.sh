#!/usr/bin/env bash
# End-to-end Phase 1 acceptance demo (PRD §7):
#   1. provider publishes, consumer registers usage
#   2. a PR removing a CONSUMED field is BLOCKED (exit 1)
#   3. a PR removing an UNCONSUMED field PASSES (exit 0)
#
# Requires: go, javac/java 11+, jackson jars (see WIREFIT_JARS_DIR, default /tmp/jars).
set -euo pipefail
cd "$(dirname "$0")/.."

./extractors/java/fetch-jars.sh
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
CP="$JARS/jackson-core-2.17.2.jar:$JARS/jackson-databind-2.17.2.jar:$JARS/jackson-annotations-2.17.2.jar:$JARS/jackson-datatype-jdk8-2.17.2.jar:$JARS/jsr305-3.0.2.jar"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

step() { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

step "build wirefit + compile demo DTOs (wirefit extract bootstraps its own extractor)"
go build -o "$WORK/wirefit" ./cmd/wirefit
javac --release 11 -cp "$CP" -d "$WORK/provider-v1" extractors/java/fixtures/src/com/acme/orders/*.java
javac --release 11 -cp "$CP" -d "$WORK/consumer" examples/web-app/src/com/acme/orders/web/OrderView.java

step "init contracts repo"
REPO="$WORK/contracts-repo"
mkdir -p "$REPO" && git -C "$REPO" init -q && git -C "$REPO" config user.email demo@wirefit && git -C "$REPO" config user.name wirefit-demo

step "provider: extract + publish v1"
"$WORK/wirefit" extract -f examples/order-service/contracts.yaml --classpath "$WORK/provider-v1:$JARS/jsr305-3.0.2.jar" --ir "$WORK/ir-provider-v1"
"$WORK/wirefit" publish -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-provider-v1"

step "consumer: extract + check + publish (registers usage of order_id, customer_email)"
"$WORK/wirefit" extract -f examples/web-app/contracts.yaml --classpath "$WORK/consumer:$JARS/jsr305-3.0.2.jar" --ir "$WORK/ir-consumer"
"$WORK/wirefit" check -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer"
"$WORK/wirefit" publish -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer"

step "TS consumer: extract + check (note the int64->float64 lossiness warning) + publish"
"$WORK/wirefit" extract -f examples/web-app-ts/contracts.yaml --project examples/web-app-ts --ir "$WORK/ir-consumer-ts"
"$WORK/wirefit" check -f examples/web-app-ts/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer-ts"
"$WORK/wirefit" publish -f examples/web-app-ts/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer-ts"

step "Zod consumer: extract (schema runtime-imported, z.uuid -> uuid scalar) + check + publish"
[ -d examples/web-app-zod/node_modules ] || (cd examples/web-app-zod && npm install --no-audit --no-fund --silent)
"$WORK/wirefit" extract -f examples/web-app-zod/contracts.yaml --project examples/web-app-zod --ir "$WORK/ir-consumer-zod"
"$WORK/wirefit" check -f examples/web-app-zod/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer-zod"
"$WORK/wirefit" publish -f examples/web-app-zod/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-consumer-zod"

step "provider PR #1: remove CONSUMED field customer_email -> must be BLOCKED (by ALL THREE consumers)"
mkdir -p "$WORK/src-v2" && cp extractors/java/fixtures/src/com/acme/orders/*.java "$WORK/src-v2/"
grep -v 'customerEmail' "$WORK/src-v2/OrderResponse.java" > "$WORK/src-v2/OrderResponse.tmp"
mv "$WORK/src-v2/OrderResponse.tmp" "$WORK/src-v2/OrderResponse.java"
javac --release 11 -cp "$CP" -d "$WORK/provider-v2" "$WORK/src-v2/"*.java
"$WORK/wirefit" extract -f examples/order-service/contracts.yaml --classpath "$WORK/provider-v2:$JARS/jsr305-3.0.2.jar" --ir "$WORK/ir-provider-v2"
if "$WORK/wirefit" check -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-provider-v2"; then
  echo "DEMO FAILED: breaking change was not blocked"; exit 1
else
  [ $? -eq 1 ] && echo ">>> correctly blocked (exit 1)"
fi

step "provider PR #1b: same removal WITH a recorded override -> allowed as warning"
cat > "$WORK/overrides.yaml" <<OVR
overrides:
  - interaction: orders.get-order
    path: $.customer_email
    rule: field-removed
    downgrade-to: warning
    justification: coordinated removal JIRA-123, all consumers migrate this sprint
    expires: "$(date -d '+30 days' +%Y-%m-%d 2>/dev/null || date -v+30d +%Y-%m-%d)"
OVR
"$WORK/wirefit" check -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-provider-v2" --overrides "$WORK/overrides.yaml"
echo ">>> override let it through as warning (exit 0) — gate stays on for everything else"

step "provider PR #2: remove UNCONSUMED field coupon_code -> must PASS"
mkdir -p "$WORK/src-v3" && cp extractors/java/fixtures/src/com/acme/orders/*.java "$WORK/src-v3/"
grep -v -e 'couponCode' -e 'JsonInclude.Include.NON_NULL' "$WORK/src-v3/OrderResponse.java" > "$WORK/src-v3/OrderResponse.tmp"
mv "$WORK/src-v3/OrderResponse.tmp" "$WORK/src-v3/OrderResponse.java"
javac --release 11 -cp "$CP" -d "$WORK/provider-v3" "$WORK/src-v3/"*.java
"$WORK/wirefit" extract -f examples/order-service/contracts.yaml --classpath "$WORK/provider-v3:$JARS/jsr305-3.0.2.jar" --ir "$WORK/ir-provider-v3"
"$WORK/wirefit" check -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-provider-v3"
echo ">>> correctly passed (exit 0)"

step "DEMO PASSED"
git -C "$REPO" log --oneline
