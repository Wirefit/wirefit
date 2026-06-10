#!/usr/bin/env bash
# Fetch the pinned extractor/fixture jars (checksum-verified) into WIREFIT_JARS_DIR.
# Used by test.sh and examples/demo.sh; `wirefit extract` itself bootstraps its own
# cache and does not need this.
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

fetch com/fasterxml/jackson/core/jackson-core/2.17.2/jackson-core-2.17.2.jar \
  721a189241dab0525d9e858e5cb604d3ecc0ede081e2de77d6f34fa5779a5b46
fetch com/fasterxml/jackson/core/jackson-databind/2.17.2/jackson-databind-2.17.2.jar \
  c04993f33c0f845342653784f14f38373d005280e6359db5f808701cfae73c0c
fetch com/fasterxml/jackson/core/jackson-annotations/2.17.2/jackson-annotations-2.17.2.jar \
  873a606e23507969f9bbbea939d5e19274a88775ea5a169ba7e2d795aa5156e1
fetch com/fasterxml/jackson/datatype/jackson-datatype-jdk8/2.17.2/jackson-datatype-jdk8-2.17.2.jar \
  aaa98d3edabf50426bd822fad1442fbdada6e470969326cbcab5c2798f1738d9
fetch com/google/code/findbugs/jsr305/3.0.2/jsr305-3.0.2.jar \
  766ad2a0783f2687962c8ad74ceecc38a28b9f72a2d085ee438b7813e928d0c7
