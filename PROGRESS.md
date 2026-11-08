# Progress

## Vision

`k8s-multi-tenant-platform` is a namespace-per-tenant multi-tenancy reference
implementation for Kubernetes. It shows how to isolate tenants sharing a
single cluster using only upstream Kubernetes primitives — no service mesh,
no external policy engine:

- A dedicated **Namespace** per tenant
- **ResourceQuota** to cap aggregate CPU/memory/object counts
- **LimitRange** to set sane per-container defaults and bounds
- **NetworkPolicy** to deny all ingress/egress by default, then allow only
  same-namespace traffic and DNS resolution
- Per-tenant **RBAC** (`Role` + `RoleBinding`) scoping a tenant admin subject
  to their own namespace
- A **CLI** (`tenantctl`) that generates and applies this entire bundle for a
  new tenant in one command, can reconcile an existing tenant to a changed
  spec, and can list/delete tenants cleanly

The goal is a reference an engineer can point to and say "this is how I'd
bootstrap tenant isolation on a shared cluster," backed by real, runnable
code and tests — not just YAML samples.

## Architecture

```
k8s-multi-tenant-platform/
  cmd/tenantctl/          CLI entrypoint and subcommands (Cobra)
  internal/tenant/
    types.go              TenantSpec: the declarative shape of a tenant
    validate.go           Validation rules for TenantSpec
    manifests.go          Pure builder functions: TenantSpec -> typed k8s API objects
    render.go             Marshal built objects to YAML (for `generate`), plus the
                           line-level diff used by `update --diff`
    client.go             Thin wrapper over kubernetes.Interface: Create/Update/Delete/
                           List/Diff tenants
    file.go              LoadSpecFile: read a TenantSpec from a YAML file (GitOps mode)
  examples/               Sample GitOps tenant spec file for `--file`
  scripts/                Local dev helpers: kind-up.sh (cluster bring-up), demo.sh (full
                          CLI workflow), capture_screenshots.py (regenerates docs/screenshots)
  docs/screenshots/       Preview images embedded in README.md
```

Design choices:

- **Typed client-go clientsets, not dynamic client.** Every object this
  project manages (Namespace, ResourceQuota, LimitRange, NetworkPolicy,
  Role, RoleBinding) has a typed clientset in `client-go`. Using typed
  clients keeps `internal/tenant/client.go` simple and avoids pulling in a
  RESTMapper/discovery layer that would add complexity without adding much
  real value for a fixed, known set of resource kinds.
- **Manifest building is pure and cluster-independent.** `manifests.go`
  takes a `TenantSpec` and returns typed Go structs — no cluster, no I/O.
  This is what makes `tenantctl generate` work offline and what makes the
  bundle trivially unit-testable.
- **`tenant.Client` takes a `kubernetes.Interface`**, not a concrete
  `*kubernetes.Clientset`. Production code constructs it from a kubeconfig;
  tests construct it from `k8s.io/client-go/kubernetes/fake`. This is the
  seam that makes `create`/`delete`/`list` testable without a real cluster.
- **Tenant identity is a label, not a naming convention.** Every object
  carries `tenant-platform.io/tenant=<name>` and
  `tenant-platform.io/managed-by=tenantctl`, so `list` and `delete` work by
  selector rather than by guessing namespace names.
- **`update` reconciles rather than patches.** `UpdateTenant` always builds
  the full desired `Bundle` from a `Spec` (same as `create`) and makes the
  live cluster match it exactly, including deleting a NetworkPolicy that's
  no longer in the desired set (e.g. a removed `--allow-namespace`). This
  keeps "what's on the cluster" always explainable by "what's in the spec,"
  rather than accumulating drift from partial, field-by-field patches.
- **`--diff` strips API server bookkeeping before comparing.** A live
  object's `resourceVersion`, `uid`, `managedFields`, `creationTimestamp`,
  and a freshly-built object's `apiVersion`/`kind` (which a typed client-go
  `Get` never populates) are noise that would appear in every diff whether
  or not anything actually changed. `DiffTenant` strips both before
  rendering, so the output is only the fields that actually drifted.

## Phased build plan

- [x] **Phase 1 — Scaffold & core foundation** (Night 1)
  - [x] Go module, directory layout, Makefile, CI workflow, LICENSE, .gitignore
  - [x] `TenantSpec` type + validation (name, quota, limit range, admin subject)
  - [x] Manifest builders: Namespace, ResourceQuota, LimitRange, NetworkPolicy
        (default-deny + allow-same-namespace + allow-dns), Role, RoleBinding
  - [x] YAML rendering of the full bundle
  - [x] `tenant.Client` (typed client-go wrapper): CreateTenant / DeleteTenant / ListTenants
  - [x] CLI: `tenantctl generate`, `create`, `delete`, `list`
  - [x] Unit tests for validation, manifest builders, rendering, and client
        operations against a fake clientset
  - [x] End-to-end smoke test against a real local `kind` cluster
  - [x] README with quick start, preview screenshots, and docs

- [x] **Phase 2 — Policy depth & safety** (Night 2)
  - [x] Cross-namespace allow-list support (`Spec.AllowedNamespaces`,
        `--allow-namespace`, an `allow-cross-namespace` NetworkPolicy built
        only when the list is non-empty, so a tenant with no allow-list
        gets byte-identical output to before this feature existed)
  - [x] `tenantctl update` to reconcile quota/limit/network-policy/RBAC
        changes on an existing tenant (`Client.UpdateTenant`,
        `reconcileNetworkPolicies`)
  - [x] Dry-run / diff mode: `--dry-run` on `create`/`update` (renders the
        bundle, never contacts the cluster) and `--diff` on `update`
        (`Client.DiffTenant`, a line-level diff against live cluster state)
  - [x] Config-file driven tenant specs: `--file`/`-f` on
        `generate`/`create`/`update` (`tenant.LoadSpecFile`), see
        [`examples/tenant.yaml`](examples/tenant.yaml)
  - [x] Admission-time safety checks: `DeleteTenant` and `UpdateTenant` both
        refuse to act on a namespace that isn't labeled
        `tenant-platform.io/managed-by=tenantctl` (the `DeleteTenant` guard
        shipped in Phase 1; `UpdateTenant` adds the same guard)
  - [x] `scripts/kind-up.sh`, `scripts/demo.sh`, and
        `scripts/capture_screenshots.py` added (previously referenced in
        this file's architecture section but missing from the repo)
  - [x] Unit tests, `go vet`, `gofmt -l .` all pass; every Phase 2 command
        exercised for real against a local `kind` cluster, including
        `update --diff` showing real drift and `update` deleting a
        NetworkPolicy that was removed from the desired spec
  - [x] README and preview screenshots updated for all Phase 2 commands

- [ ] **Phase 3 — Multi-tenant fleet view** (Night 3)
  - [ ] `tenantctl status` reporting live quota usage vs. limits per tenant
        (note: `update --diff` already gives spec-vs-live drift for a single
        tenant; `status` should focus on usage numbers, not object diffing)
  - [ ] `tenantctl list` enhancement: show quota utilization per tenant
  - [ ] Optional Helm chart packaging of the same primitives

- [ ] **Phase 4 — Hardening & polish** (Night 4+)
  - [ ] PodSecurity admission labels per tenant
  - [ ] Integration tests via envtest in CI
  - [ ] Documentation pass: architecture diagram, troubleshooting guide

## Resume point for next session

Phase 1 and Phase 2 are both complete. The CLI now has five commands
(`generate`, `create`, `update`, `delete`, `list`), cross-namespace
NetworkPolicy allow-listing, `--dry-run`/`--diff` modes, and GitOps
config-file specs via `--file`. Everything in Phase 2 was exercised for
real against a local `kind` cluster, not just the fake clientset.

Start Phase 3 by reading `cmd/tenantctl/cmd_list.go` and
`internal/tenant/client.go`'s `ListTenants`/`Info` — `tenantctl status` will
need per-tenant quota usage, which means fetching each managed namespace's
`ResourceQuota.Status.Used` (already present on the live object; `Info`
currently only carries `Name`/`Phase`/`CreatedAt`, so it'll need a new
field or a separate `Usage` type). Keep `status` read-only and separate
from `update --diff`, which already covers full-bundle drift detection —
`status` is specifically about quota numbers, not another diff mode.

STATUS: IN_PROGRESS
