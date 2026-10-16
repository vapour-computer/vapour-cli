#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/cmd/llm_fallback.txt"
TMP="$(mktemp)"
URL="https://raw.githubusercontent.com/vapour-computer/vapour-dev-docs/main/book/llm.txt"
HEADER="# Vapour developer guide for LLMs"

cleanup() {
  rm -f "$TMP"
}
trap cleanup EXIT

curl -fsSL "$URL" -o "$TMP"

if ! head -n 1 "$TMP" | grep -Fxq "$HEADER"; then
  echo "sync-llm.sh: downloaded file is missing required header: $HEADER" >&2
  exit 1
fi

mv "$TMP" "$OUT"
echo "synced $OUT from $URL"
