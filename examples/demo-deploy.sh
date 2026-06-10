#!/usr/bin/env bash
# Phase 4 acceptance demo (PRD §7): the scenario HEAD-vs-HEAD checks CANNOT
# catch. The consumer migrates on main, the provider removes the field —
# `wirefit check` is green. But the OLD consumer still runs in production:
# `can-i-deploy` blocks until the consumer's deploy is recorded.
set -euo pipefail
cd "$(dirname "$0")/.."

./extractors/java/fetch-jars.sh
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
CP="$JARS/jackson-core-2.17.2.jar:$JARS/jackson-databind-2.17.2.jar:$JARS/jackson-annotations-2.17.2.jar:$JARS/jackson-datatype-jdk8-2.17.2.jar:$JARS/jsr305-3.0.2.jar"
J="$JARS/jsr305-3.0.2.jar"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
step() { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

step "setup: build, init contracts repo, publish provider v1 + consumer v1, record deploys"
go build -o "$WORK/wirefit" ./cmd/wirefit
javac --release 11 -cp "$CP" -d "$WORK/prov-v1" extractors/java/fixtures/src/com/acme/orders/*.java
javac --release 11 -cp "$CP" -d "$WORK/cons-v1" examples/web-app/src/com/acme/orders/web/OrderView.java
REPO="$WORK/repo"; mkdir -p "$REPO"; git -C "$REPO" init -q
git -C "$REPO" config user.email demo@wirefit; git -C "$REPO" config user.name wirefit-demo

"$WORK/wirefit" extract -f examples/order-service/contracts.yaml --classpath "$WORK/prov-v1:$J" --ir "$WORK/ir-prov-v1" >/dev/null
"$WORK/wirefit" publish -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-prov-v1" >/dev/null
"$WORK/wirefit" extract -f examples/web-app/contracts.yaml --classpath "$WORK/cons-v1:$J" --ir "$WORK/ir-cons-v1" >/dev/null
"$WORK/wirefit" publish -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-cons-v1" >/dev/null
"$WORK/wirefit" record-deploy -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --env production
"$WORK/wirefit" record-deploy -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --env production

step "consumer migrates on main: stops reading customer_email (published, NOT yet deployed)"
mkdir "$WORK/cons-src-v2"
cat > "$WORK/cons-src-v2/OrderView.java" <<'JAVA'
package com.acme.orders.web;

import com.fasterxml.jackson.annotation.JsonProperty;
import javax.annotation.Nonnull;
import java.util.UUID;

/** v2: migrated off customer_email. */
public class OrderView {
    @Nonnull
    @JsonProperty("order_id")
    public UUID orderId;
}
JAVA
javac --release 11 -cp "$CP" -d "$WORK/cons-v2" "$WORK/cons-src-v2/OrderView.java"
"$WORK/wirefit" extract -f examples/web-app/contracts.yaml --classpath "$WORK/cons-v2:$J" --ir "$WORK/ir-cons-v2" >/dev/null
"$WORK/wirefit" publish -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-cons-v2" >/dev/null

step "provider removes customer_email: HEAD check is GREEN (main consumer no longer reads it)"
mkdir "$WORK/prov-src-v2" && cp extractors/java/fixtures/src/com/acme/orders/*.java "$WORK/prov-src-v2/"
grep -v 'customerEmail' "$WORK/prov-src-v2/OrderResponse.java" > "$WORK/t" && mv "$WORK/t" "$WORK/prov-src-v2/OrderResponse.java"
javac --release 11 -cp "$CP" -d "$WORK/prov-v2" "$WORK/prov-src-v2/"*.java
"$WORK/wirefit" extract -f examples/order-service/contracts.yaml --classpath "$WORK/prov-v2:$J" --ir "$WORK/ir-prov-v2" >/dev/null
"$WORK/wirefit" check -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --ir "$WORK/ir-prov-v2" | tail -1
echo ">>> HEAD check green (exit 0) — main-vs-main sees no problem"

step "but production still runs the OLD consumer: can-i-deploy must BLOCK"
if "$WORK/wirefit" can-i-deploy -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --env production --ir "$WORK/ir-prov-v2"; then
  echo "DEMO FAILED: can-i-deploy did not block"; exit 1
else
  echo ">>> correctly blocked (exit 1): deployed web-app still reads customer_email"
fi

step "consumer v2 deploys to production (record-deploy) -> provider is now SAFE"
"$WORK/wirefit" record-deploy -f examples/web-app/contracts.yaml --contracts-repo "$REPO" --env production
"$WORK/wirefit" can-i-deploy -f examples/order-service/contracts.yaml --contracts-repo "$REPO" --env production --ir "$WORK/ir-prov-v2" | tail -1
echo ">>> correctly unblocked (exit 0)"

step "deployed compatibility matrix"
"$WORK/wirefit" matrix --contracts-repo "$REPO"

step "DEPLOY DEMO PASSED"
