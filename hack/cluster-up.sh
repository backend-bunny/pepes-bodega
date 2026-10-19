#!/usr/bin/env bash
# Create the kind cluster (idempotent) and install KEDA.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER=${KIND_CLUSTER:-pepes-bodega}
NS=bodega-dev
KEDA_VERSION=${KEDA_VERSION:-2.21.0}

if ! kind get clusters | grep -qx "$CLUSTER"; then
  kind create cluster --name "$CLUSTER" --config deploy/dev/kind.yaml
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl config set-context --current --namespace "$NS" >/dev/null

if ! helm status keda -n keda >/dev/null 2>&1; then
  helm repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
  helm repo update kedacore >/dev/null
  helm install keda kedacore/keda --namespace keda --create-namespace --version "$KEDA_VERSION" --wait
fi

echo "cluster $CLUSTER ready (namespace $NS). Next: devbox run dev"
