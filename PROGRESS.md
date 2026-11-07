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
  new tenant in one command, and can list/delete tenants cleanly

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
    render.go             Marshal built objects to YAML (for `generate`)
    client.go             Thin wrapper over kubernetes.Interface: Create/Delete/List tenants
  scripts/                Local dev helpers (kind cluster bring-up, demo run, screenshot capture)
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

- [ ] **Phase 2 — Policy depth & safety** (Night 2)
  - [ ] Cross-namespace allow-list support (e.g. shared ingress controller namespace)
  - [ ] `tenantctl update` to reconcile quota/limit changes on an existing tenant
  - [ ] Dry-run / diff mode (`--dry-run=server`, `--diff`)
  - [ ] Config-file driven tenant specs (`tenantctl create -f tenant.yaml`) for GitOps use
  - [ ] Admission-time safety checks (block delete of non-`tenantctl`-managed namespaces)

- [ ] **Phase 3 — Multi-tenant fleet view** (Night 3)
  - [ ] `tenantctl status` reporting live quota usage vs. limits per tenant
  - [ ] Drift detection between declared spec and live cluster state
  - [ ] Optional Helm chart packaging of the same primitives

- [ ] **Phase 4 — Hardening & polish** (Night 4+)
  - [ ] PodSecurity admission labels per tenant
  - [ ] Integration tests via envtest in CI
  - [ ] Documentation pass: architecture diagram, troubleshooting guide

## Resume point for next session

Phase 1 is complete: scaffold, manifest generation, client-go wrapper, CLI,
unit tests (passing), and a real `kind` cluster smoke test all exist and
work. Start Phase 2 by reading `internal/tenant/manifests.go` and adding the
cross-namespace allow-list to `buildNetworkPolicies` first — the `create`/
`update` split in the CLI depends on that shape being settled before the
GitOps config-file flag is added.

STATUS: IN_PROGRESS
