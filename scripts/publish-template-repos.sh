#!/usr/bin/env bash
set -euo pipefail

# Creates/pushes official template repos in a GitHub org using gh CLI.
# Usage:
#   scripts/publish-template-repos.sh [org]

ORG="${1:-vapour-computer}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d /tmp/opencode/vapour-templates.XXXXXX)"

cleanup() {
  rm -rf "$WORK"
}
trap cleanup EXIT

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

need git
need gh

render_repo() {
  local engine="$1"
  local repo="vapour-template-$engine"
  local src="$ROOT/internal/scaffold/templates/$engine"
  local dst="$WORK/$repo"
  local title

  case "$engine" in
    bevy) title="Bevy Starter" ;;
    godot) title="Godot Starter" ;;
    unity) title="Unity Starter" ;;
    *) echo "unsupported engine: $engine" >&2; exit 1 ;;
  esac

  mkdir -p "$dst"
  cp -R "$src/." "$dst/"

  # Render simple *.tmpl files.
  while IFS= read -r -d '' f; do
    out="${f%.tmpl}"
    sed "s/{{.Title}}/$title/g" "$f" > "$out"
    rm "$f"
  done < <(find "$dst" -type f -name '*.tmpl' -print0)

  git -C "$dst" init -b main >/dev/null
  git -C "$dst" add .
  git -C "$dst" -c user.name='Vapour' -c user.email='dev@vapour.ink' commit -m "Initial $engine template" >/dev/null

  if gh repo view "$ORG/$repo" >/dev/null 2>&1; then
    echo "repo exists: $ORG/$repo (pushing updates)"
    git -C "$dst" remote add origin "git@github.com:$ORG/$repo.git"
    git -C "$dst" push -u origin main --force-with-lease
  else
    echo "creating repo: $ORG/$repo"
    gh repo create "$ORG/$repo" --public --description "Official Vapour $engine template" --source "$dst" --push
  fi
}

render_repo bevy
render_repo godot
render_repo unity

echo "Done. Template repos are ready in $ORG."
