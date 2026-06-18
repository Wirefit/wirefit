#!/usr/bin/env bash
# Fetch the pinned extractor/fixture jars (checksum-verified) into WIREFIT_JARS_DIR.
# Used by test.sh (and by the demos in the wirefit/examples repo); `wirefit extract`
# itself bootstraps its own cache and does not need this.
set -euo pipefail
JARS="${WIREFIT_JARS_DIR:-/tmp/jars}"
mkdir -p "$JARS"

fetch() { # path sha256
  local file="${1##*/}"
  if [ -f "$JARS/$file" ] && echo "$2  $JARS/$file" | sha256sum -c --quiet - 2>/dev/null; then
    return
  fi
  curl -sfL --max-time 60 -o "$JARS/$file" "https://repo1.maven.org/maven2/$1"
  echo "$2  $JARS/$file" | sha256sum -c --quiet -
}

fetch com/fasterxml/jackson/core/jackson-core/2.22.0/jackson-core-2.22.0.jar \
  d2e8dd4df1e0f61b786ea06792f5bf4235d8278f158f3be6e997e955931c0c98
fetch com/fasterxml/jackson/core/jackson-databind/2.22.0/jackson-databind-2.22.0.jar \
  3520a0351f294699e3e1b7a37c7a726afd81e1a89ae702ac7d47ff347fd2ecbf
fetch com/fasterxml/jackson/core/jackson-annotations/2.22/jackson-annotations-2.22.jar \
  21ddb598807d3a51a876704eb979d9296e1c6a6f47ab1826ff88c6d6a127a2d0
fetch com/fasterxml/jackson/datatype/jackson-datatype-jdk8/2.22.0/jackson-datatype-jdk8-2.22.0.jar \
  f1051ed0938aa5edb7567ab19c2c7e1fade58f7fbad43d99a74cb506389c1ac5
fetch jakarta/annotation/jakarta.annotation-api/3.0.0/jakarta.annotation-api-3.0.0.jar \
  b01f55552284cfb149411e64eabca75e942d26d2e1786b32914250e4330afaa2
