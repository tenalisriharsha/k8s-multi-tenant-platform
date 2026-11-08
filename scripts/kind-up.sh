#!/usr/bin/env bash
# Bring up a local kind cluster for trying tenantctl against a real
# Kubernetes API server. Requires kind and kubectl on PATH.
set -euo pipefail

CLUSTER_NAME="${1:-tenant-demo}"

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "kind cluster '$CLUSTER_NAME' already exists, reusing it"
else
  kind create cluster --name "$CLUSTER_NAME"
fi

echo "kubeconfig context: kind-$CLUSTER_NAME"
kubectl --context "kind-$CLUSTER_NAME" get nodes
