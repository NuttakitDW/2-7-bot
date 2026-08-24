#!/usr/bin/env bash
#
# Clone and build the upstream poker-arena engine at the pinned commit.
#
# Production runs this same engine contract inside a sandbox, so a clean local
# match is real evidence — and it costs nothing per iteration, unlike a hosted
# competition. The checkout lives in third_party/ and is gitignored.

set -euo pipefail

cd "$(dirname "$0")/.."

ENGINE_SHA="$(sed -n 's/^ENGINE_SHA[[:space:]]*:=[[:space:]]*//p' Makefile)"
ENGINE_REPO="$(sed -n 's/^ENGINE_REPO[[:space:]]*:=[[:space:]]*//p' Makefile)"
ENGINE_DIR="$(sed -n 's/^ENGINE_DIR[[:space:]]*:=[[:space:]]*//p' Makefile)"
ENGINE_BIN="$ENGINE_DIR/target/release/poker-arena"

if [ -x "$ENGINE_BIN" ] && [ "$(git -C "$ENGINE_DIR" rev-parse HEAD 2>/dev/null || true)" = "$ENGINE_SHA" ]; then
  echo "engine: already built at $ENGINE_SHA"
  exit 0
fi

command -v cargo >/dev/null || {
  echo "engine: cargo not found — install Rust from https://rustup.rs" >&2
  exit 1
}

if [ ! -d "$ENGINE_DIR/.git" ]; then
  echo "engine: cloning $ENGINE_REPO"
  mkdir -p "$(dirname "$ENGINE_DIR")"
  git clone --quiet "$ENGINE_REPO" "$ENGINE_DIR"
fi

echo "engine: checking out $ENGINE_SHA"
git -C "$ENGINE_DIR" fetch --quiet origin
git -C "$ENGINE_DIR" checkout --quiet "$ENGINE_SHA"

echo "engine: building (first build takes a few minutes)"
cargo build --release --manifest-path "$ENGINE_DIR/Cargo.toml" -p poker-arena-cli

echo "engine: ready at $ENGINE_BIN"
"$ENGINE_BIN" --version 2>/dev/null || true
