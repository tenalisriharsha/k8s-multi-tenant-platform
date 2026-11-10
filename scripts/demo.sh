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
echo "=== pod security: namespace labels and a rejected privileged pod ==="
kubectl --context "$CTX" get namespace "$TENANT" -o jsonpath='{.metadata.labels}'
echo
for i in $(seq 1 30); do
  kubectl --context "$CTX" get serviceaccount default -n "$TENANT" >/dev/null 2>&1 && break
  sleep 1
done
kubectl --context "$CTX" run privileged-test -n "$TENANT" --image=busybox:1.36 --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"privileged-test","image":"busybox:1.36","securityContext":{"privileged":true}}]}}' \
  || true
kubectl --context "$CTX" delete pod privileged-test -n "$TENANT" --ignore-not-found --wait=false >/dev/null

echo
echo "=== list ==="
"$BIN" list --context "$CTX"

echo
echo "=== kubectl get all isolation objects ==="
kubectl --context "$CTX" get resourcequota,limitrange,networkpolicy,role,rolebinding -n "$TENANT"

echo
echo "=== status: live quota usage vs. limits ==="
"$BIN" status "$TENANT" --context "$CTX"

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
