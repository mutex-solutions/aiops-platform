#!/usr/bin/env bash
# Render one archify source to an interactive HTML and a PNG screenshot of the diagram.
# Usage: scripts/diagrams/render.sh <id>     (reads docs/architecture/diagrams/src/<id>.json)
# Needs: node, Google Chrome, the archify skill, and a puppeteer-core checkout.
set -euo pipefail
ARCHIFY="${ARCHIFY:-$HOME/.agents/skills/archify/bin/archify.mjs}"
if [ -z "${PUPPETEER_CORE:-}" ]; then
  for d in "$HOME"/.npm/_npx/*/node_modules/puppeteer-core; do
    [ -f "$d/lib/esm/puppeteer/puppeteer-core.js" ] && PUPPETEER_CORE="$d" && break
  done
fi
: "${PUPPETEER_CORE:?set PUPPETEER_CORE to a puppeteer-core package directory}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
D="$ROOT/docs/architecture/diagrams"
id="$1"
type="$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).diagram_type)' "$D/src/$id.json")"
node "$ARCHIFY" deliver "$type" "$D/src/$id.json" "$D/html/$id.html" --quality showcase --json >/dev/null
node "$ROOT/scripts/diagrams/snap.mjs" "$PUPPETEER_CORE" "$D/html/$id.html" "$D/png/$id.png" 2
