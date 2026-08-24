#!/usr/bin/env bash
#
# Verify (or refresh) the vendored upstream protocol documents.
#
#   sync-docs.sh --check    re-fetch at the pinned SHA and diff; non-zero on drift
#   sync-docs.sh --update   overwrite the local copies and print new digests
#
# The pin lives in the Makefile as ENGINE_SHA so there is exactly one place to
# bump. Upstream publishes no releases, so a commit SHA is the only stable pin.

set -euo pipefail

cd "$(dirname "$0")/.."

SHA="$(sed -n 's/^ENGINE_SHA[[:space:]]*:=[[:space:]]*//p' Makefile)"
[ -n "$SHA" ] || { echo "sync-docs: ENGINE_SHA not found in Makefile" >&2; exit 2; }

BASE="https://raw.githubusercontent.com/mixedsolver/poker-arena/$SHA"
FILES=(
  "WIRE_PROTOCOL.md:docs/protocol/WIRE_PROTOCOL.md"
  "WIRE_PROTOCOL_OFC.md:docs/protocol/WIRE_PROTOCOL_OFC.md"
  "examples/bot.py:docs/protocol/examples/bot.py"
  "examples/ofc_bot.py:docs/protocol/examples/ofc_bot.py"
)

MODE="${1:---check}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

drift=0
for entry in "${FILES[@]}"; do
  remote="${entry%%:*}"
  local_path="${entry#*:}"
  out="$TMP/$(basename "$local_path")"

  if ! curl -sfL -o "$out" "$BASE/$remote"; then
    echo "sync-docs: FETCH FAILED $BASE/$remote" >&2
    exit 2
  fi

  case "$MODE" in
    --update)
      mkdir -p "$(dirname "$local_path")"
      cp "$out" "$local_path"
      printf '%s  %s\n' "$(shasum -a 256 "$local_path" | cut -d' ' -f1)" "$local_path"
      ;;
    --check)
      if [ ! -f "$local_path" ]; then
        echo "sync-docs: MISSING $local_path" >&2
        drift=1
      elif ! diff -q "$local_path" "$out" >/dev/null; then
        echo "sync-docs: DRIFT $local_path" >&2
        diff -u "$local_path" "$out" || true
        drift=1
      fi
      ;;
    *)
      echo "usage: sync-docs.sh [--check|--update]" >&2
      exit 2
      ;;
  esac
done

if [ "$MODE" = "--update" ]; then
  echo "sync-docs: updated at $SHA — update the digest table in docs/SOURCES.md"
  exit 0
fi

# Informational: has upstream moved past the pin? Not a failure — upgrading is a
# deliberate act, because a protocol change may need code changes alongside it.
head_sha="$(curl -sfL https://api.github.com/repos/mixedsolver/poker-arena/commits/master \
  | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -1 || true)"
if [ -n "$head_sha" ] && [ "$head_sha" != "$SHA" ]; then
  echo "sync-docs: note — upstream master is now $head_sha (pinned: $SHA)"
fi

if [ "$drift" -ne 0 ]; then
  echo "sync-docs: vendored docs do not match the pinned commit" >&2
  exit 1
fi
echo "sync-docs: vendored docs match $SHA"
