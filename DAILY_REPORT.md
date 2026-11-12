# Daily Report

Summary of what was built across all sessions on `k8s-multi-tenant-platform`,
a namespace-per-tenant multi-tenancy reference implementation for Kubernetes
and its CLI, `tenantctl`.

## What was built, session by session

**Session 1 — Scaffold & core foundation.** Go module and directory layout,
`TenantSpec` with validation, pure manifest builders for Namespace,
ResourceQuota, LimitRange, and default-deny/allow-same-namespace/allow-dns
NetworkPolicies, YAML rendering, a typed `client-go`-backed `tenant.Client`
(Create/Delete/List), and the first four `tenantctl` subcommands (`generate`,
`create`, `delete`, `list`). Unit tests against validation, builders,
rendering, and a fake clientset; a real `kind` cluster smoke test; the
initial README with quick start and preview screenshots.

**Session 2 — Policy depth & safety.** Cross-namespace NetworkPolicy
allow-listing (`--allow-namespace`), `tenantctl update` to reconcile an
existing tenant's quota/limit-range/network-policy/RBAC against a changed
spec, `--dry-run` and `--diff` modes, GitOps-style config-file specs
(`--file`), and an admission-time safety guard so `delete`/`update` refuse to
touch a namespace not labeled `tenant-platform.io/managed-by=tenantctl`.
Added `scripts/kind-up.sh`, `scripts/demo.sh`, and
`scripts/capture_screenshots.py`. Every Phase 2 command was exercised for
real against a local `kind` cluster, including `update --diff` showing real
drift and `update` deleting a NetworkPolicy removed from the desired spec.

**Session 3 — Multi-tenant fleet view.** `tenantctl status [name]` reporting
live `ResourceQuota.Status` usage versus hard limits per tenant, and a
`QUOTA` column on `tenantctl list` showing each tenant's single
most-utilized resource for an at-a-glance fleet view. A real bug surfaced
here — the `QUOTA` column leaked the raw resource-key prefix (e.g.
`requests.cpu` instead of `cpu`) — found by the independent test suite and
fixed in `peakUtilization`'s `shortResourceName`. Helm chart packaging was
scoped for this phase and deliberately deferred: a single static binary
plus `generate | kubectl apply -f -` covered every real need without it.

**Session 4 — Hardening & polish.** Per-tenant Pod Security Admission levels
(`--pod-security`, default `restricted`) rendered as the standard
`pod-security.kubernetes.io/{enforce,audit,warn}` namespace labels. This
closed a real gap: `update`/`diff` had never touched the Namespace object at
all before this, so a PSA level change could be set at `create` time but
never reconciled or previewed afterward — both now diff and reconcile the
namespace's labels. A documentation pass added an ASCII architecture diagram
and a troubleshooting guide built from this project's own real error
messages. `sigs.k8s.io/controller-runtime`'s `envtest` was wired in as
infrastructure (`internal/testutil`'s `NewEnvtestEnvironment`/
`NewClientset`, `make envtest`, and a CI job that installs the kubebuilder
test binaries and runs the `envtest`-tagged suite) — proven working against
a live ephemeral API server before being committed, closing a `go.mod`
dependency change that had been blocking the test suite itself from landing.

**Session 5 — Closing the integration-test gap.** The one item left open
from session 4: the `envtest`-tagged integration test file itself
(`tests/envtest_test.go`), exercising `CreateTenant`/`UpdateTenant`/
`ListTenants`/`GetTenant`/`DiffTenant`/`DeleteTenant` against a real,
ephemeral API server rather than the fake clientset the unit tests use. It
confirms behavior the fake clientset can't: a real server injects
`kubernetes.io/metadata.name` on every namespace and never echoes back
`TypeMeta` on `Get`, a full reconcile produces zero diff drift against a
real server (no `apiVersion`/`kind` exception needed, unlike the fake), and
a real `DeleteTenant` leaves the namespace `Terminating` rather than
disappearing synchronously, since `envtest` runs only `etcd` and
`kube-apiserver`, not the namespace controller that would finalize it. This
closes the project's build plan: no open items remain beyond the
explicitly-deferred Helm chart packaging.

## Test results

- `go test ./... -cover`: all unit tests pass (`cmd/tenantctl` 82.9%,
  `internal/tenant` 87.5% statement coverage).
- `go vet ./...` and `gofmt -l .`: clean.
- `make envtest` (`go test -tags envtest ./...`): all integration tests
  pass against a real, ephemeral `etcd`/`kube-apiserver` pair — namespace
  creation/labeling, full-bundle create/update/delete reconciliation, and
  zero-drift diffing, all verified against real API server behavior rather
  than the fake clientset.
- `go test -tags e2e ./tests/... -v -timeout 5m`: exercises the built
  `tenantctl` binary itself against a local `kind` cluster, per the
  README's own documented workflow; run manually (requires Docker/`kind`,
  not part of CI).
- The CLI's headline path (`generate`, `create`, `update --diff`, `status`,
  `list`, `delete`) was run for real against a local `kind` cluster across
  sessions, including a pod sized by the tenant's `LimitRange` defaults
  showing up as live usage in `status`, and a privileged pod genuinely
  rejected by the cluster's PodSecurity admission controller.

## Known limitations

- Helm chart packaging was scoped and explicitly deferred — `generate |
  kubectl apply -f -` and the Go binary cover every need seen so far.
- `tenantctl update` reconciles a tenant's labels, quota, limit range,
  network policies, and RBAC, but never renames its namespace — a tenant's
  name is immutable once created, by design.
- `status`/`list`'s usage numbers come from `ResourceQuota.Status`, which
  only a running quota controller populates; under `envtest` (no
  controller-manager) usage is always empty, which is expected and asserted
  by the integration tests rather than being a bug.
- No RBAC-impersonation test exercises the tenant-admin `Role`/`RoleBinding`
  from the bound subject's own point of view (e.g. via `kubectl auth
  can-i --as`); the RBAC objects' existence and shape are covered, not a
  live permission check.

## Future ideas

- Optional Helm chart (or `kustomize` overlay) packaging of the same
  manifest bundle, if a GitOps pipeline wants it rather than
  `generate | kubectl apply -f -` directly.
- A `tenantctl doctor`/`validate-cluster` command that checks a tenant's
  live objects against the spec it was created from without the full
  `update --diff` ceremony, for quick drift spot-checks.
- An optional per-tenant `NetworkPolicy` egress allow-list to specific
  external CIDRs, for tenants that need to reach a database or API outside
  the cluster without opening egress entirely.
