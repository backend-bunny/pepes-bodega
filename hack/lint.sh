#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
echo "== go"; go vet ./... && golangci-lint run ./...
echo "== python"; (cd workers && ruff check . && ruff format --check .)
echo "== helm"; helm lint deploy/helm/pepes-bodega -f deploy/helm/values-dev.yaml
# Evaluates the Tiltfile (incl. its YAML, with Tilt's stricter parser) without a cluster.
echo "== tiltfile"
err=$(tilt alpha tiltfile-result 2>&1 | jq -r '.Error // empty')
[[ -z "$err" ]] || { echo "$err"; exit 1; }
