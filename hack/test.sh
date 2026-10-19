#!/usr/bin/env bash
# Fast test suite: Go unit tests, Python tests without models, web typecheck.
# Set WITH_MODELS=1 to also run the slow model tests (downloads ~1 GB once).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== go"; go test ./...
echo "== workers"
marker='not models'; [[ "${WITH_MODELS:-}" == 1 ]] && marker=''
(cd workers && uv run pytest -q ${marker:+-m "$marker"})
echo "== web"; (cd web && npm run --silent check)
echo "== oidc e2e (real Dex, no models)"
LOCAL_STACK_DIR=$(mktemp -d)/stack hack/e2e-oidc.sh
