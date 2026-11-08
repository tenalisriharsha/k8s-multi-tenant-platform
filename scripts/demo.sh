#!/usr/bin/env bash
# Run the full tenantctl workflow against a real cluster: build the binary,
# bring up (or reuse) a kind cluster, onboard a tenant, inspect it, update
# its quota and network allow-list, diff a further change, then offboard
# it. Requires kind and kubectl on PATH.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLUSTER_NAME="${1:-tenant-demo}"
CTX="kind-$CLUSTER_NAME"
TENANT="demo-corp"

"$ROOT/scripts/kind-up.sh" "$CLUSTER_NAME"

echo "+ go build -o bin/tenantctl ./cmd/tenantctl"
(cd "$ROOT" && go build -o bin/tenantctl ./cmd/tenantctl)
BIN="$ROOT/bin/tenantctl"

echo
echo "=== create ==="
"$BIN" create --name "$TENANT" --admin-name "${TENANT}-admins" --context "$CTX"

echo
echo "=== list ==="
"$BIN" list --context "$CTX"

echo
echo "=== kubectl get all isolation objects ==="
kubectl --context "$CTX" get resourcequota,limitrange,networkpolicy,role,rolebinding -n "$TENANT"

echo
echo "=== update: raise the CPU quota and allow a shared ingress namespace ==="
"$BIN" update --name "$TENANT" --admin-name "${TENANT}-admins" \
  --cpu-request 3 --allow-namespace ingress-nginx --context "$CTX"
kubectl --context "$CTX" get networkpolicy -n "$TENANT"

echo
echo "=== diff: preview a further quota change without applying it ==="
"$BIN" update --name "$TENANT" --admin-name "${TENANT}-admins" \
  --cpu-request 5 --allow-namespace ingress-nginx --diff --context "$CTX"

echo
echo "=== delete ==="
"$BIN" delete --name "$TENANT" --context "$CTX"
"$BIN" list --context "$CTX"

echo
echo "demo complete. Tear the cluster down with: kind delete cluster --name $CLUSTER_NAME"
