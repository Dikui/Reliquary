#!/usr/bin/env bash
# Build frontend static assets for Docker / release packaging.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

reuse_deps=false
case "${1:-}" in
	"") ;;
	--reuse-deps) reuse_deps=true ;;
	*) echo "Usage: $0 [--reuse-deps]" >&2; exit 1 ;;
esac
[ "$#" -le 1 ] || { echo "Usage: $0 [--reuse-deps]" >&2; exit 1; }

if [ -z "${VITE_FRONTEND_COMMIT:-}" ]; then
	# shellcheck source=/dev/null
	eval "$("$PROJECT_ROOT/scripts/get-version.sh" env)"
	export VITE_FRONTEND_COMMIT="${COMMIT_ID:-unknown}"
fi

export VITE_IS_DOCKER="${VITE_IS_DOCKER:-true}"

cd "$PROJECT_ROOT/frontend"
# Keep the stamp inside node_modules so deleting dependencies invalidates it.
deps_stamp="node_modules/.reliquary-deps"
deps_fingerprint="$(node -e '
const fs = require("node:fs"), crypto = require("node:crypto");
const hash = crypto.createHash("sha256");
for (const name of ["package.json", "package-lock.json", ".npmrc"]) {
  hash.update(name); hash.update(fs.existsSync(name) ? fs.readFileSync(name) : "missing");
}
hash.update(process.version + process.platform + process.arch);
console.log(hash.digest("hex"));
')"
if [ "$reuse_deps" != true ] || [ ! -f "$deps_stamp" ] || [ "$(cat "$deps_stamp")" != "$deps_fingerprint" ]; then
	npm ci
	printf '%s\n' "$deps_fingerprint" > "$deps_stamp"
else
	echo "Reusing unchanged frontend dependencies"
fi
npm run build
