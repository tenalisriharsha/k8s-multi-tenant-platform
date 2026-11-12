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
  no longer in the desired set (e.g. a removed `--allow-namespace`) and
  updating the tenant namespace's own labels (e.g. its Pod Security
  Admission level) when they drift from the spec. This keeps "what's on
  the cluster" always explainable by "what's in the spec," rather than
  accumulating drift from partial, field-by-field patches. The namespace's
  *name* still never changes — a tenant can't be renamed by `update`, only
  relabeled.
- **`--diff` strips API server bookkeeping before comparing.** A live
  object's `resourceVersion`, `uid`, `managedFields`, `creationTimestamp`,
  a namespace's API-server-injected `kubernetes.io/metadata.name` label,
  and a freshly-built object's `apiVersion`/`kind` (which a typed client-go
  `Get` never populates) are noise that would appear in every diff whether
  or not anything actually changed. `DiffTenant` strips all of this before
  rendering, so the output is only the fields that actually drifted.
- **`status` reads ResourceQuota.Status, not Spec.** Kubernetes already
  computes live usage into `ResourceQuota.Status.Used` on every object with
  a controller watching the namespace; `status` just reads and formats it
  rather than summing pod resources itself, so it's always as fresh as the
  cluster's own quota controller. `ListTenants` now does one extra `Get`
  per tenant to pull this in; a missing `ResourceQuota` (e.g. a namespace
  mid-creation) is treated as "no usage data yet," not an error, since a
  transient gap in one tenant's quota data shouldn't fail `list`/`status`
  for every other tenant.

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

- [x] **Phase 3 — Multi-tenant fleet view** (Night 3)
  - [x] `tenantctl status [name]` reporting live quota usage vs. limits per
        tenant, one row per `ResourceQuota`-managed resource (`requests.cpu`,
        `requests.memory`, `limits.cpu`, `limits.memory`, `pods`), each as
        used/hard/utilization-percent. With no argument, reports every
        tenantctl-managed tenant; with a name, reports just that tenant and
        fails the same way `delete`/`update` do if it doesn't exist or isn't
        tenantctl-managed. Read-only, and deliberately does not duplicate
        `update --diff` (full-bundle spec-vs-live drift) — `status` is only
        about quota numbers (`Client.GetTenant`, `internal/tenant.ResourceUsage`)
  - [x] `tenantctl list` enhancement: a `QUOTA` column showing each tenant's
        single most-utilized resource (e.g. `memory 72%`) for an at-a-glance
        fleet view; the full per-resource breakdown is `status`'s job, not
        `list`'s (`internal/tenant.Client.ListTenants` now also fetches each
        tenant's `ResourceQuota` status; `cmd_list.go`'s `peakUtilization`)
  - [ ] Optional Helm chart packaging of the same primitives — deferred,
        marked optional in the original plan; not needed for `status`/`list`
        to be useful, and no indication yet that a packaging format beyond a
        single static binary + `generate | kubectl apply -f -` is wanted

- [x] **Phase 4 — Hardening & polish** (Nights 4-5)
  - [x] PodSecurity admission labels per tenant: a `PodSecurityLevel` field
        on `Spec` (`privileged`/`baseline`/`restricted`, empty = no labels,
        matching pre-Phase-4 output byte-for-byte) renders as the standard
        `pod-security.kubernetes.io/{enforce,audit,warn}` namespace labels
        (`buildNamespace`). `generate`/`create`/`update` expose it as
        `--pod-security` (default `restricted`; `none` opts out). This also
        closed a real gap: `UpdateTenant`/`DiffTenant` never touched the
        Namespace object at all before tonight (`update`'s doc comment said
        so explicitly), which meant a PSA level change could never actually
        be applied or previewed by `update`/`update --diff`. Both now
        reconcile/diff the namespace's labels (never its name), with the
        API-server-injected `kubernetes.io/metadata.name` label stripped
        from the diff so it doesn't show up as permanent noise
  - [x] Documentation pass: an ASCII architecture diagram and a
        troubleshooting guide (real error messages from this project,
        with their actual cause and fix) added to README's Architecture
        section
  - [x] envtest infrastructure: `sigs.k8s.io/controller-runtime` added as a
        direct dependency (`go.mod`/`go.sum`, resolved cleanly against the
        existing `k8s.io/*` v0.37.2 pins), plus `internal/testutil`
        (`NewEnvtestEnvironment`, `NewClientset`) — a thin, reusable
        bootstrap over `envtest.Environment`, verified for real against a
        live ephemeral API server (not just that it compiles) before being
        committed. CI (`.github/workflows/ci.yml`) now installs kubebuilder's
        `etcd`/`kube-apiserver` test binaries via `setup-envtest` and runs
        `go test -tags envtest ./...`; `make envtest` does the same locally.
        This was previously blocked on a `go.mod` change that fell outside
        the tester's test-files-only scope — that blocker is now resolved
  - [x] `tests/envtest_test.go` (`//go:build envtest`), written by the
        tester (Night 5): drives `CreateTenant`/`UpdateTenant`/
        `ListTenants`/`GetTenant`/`DiffTenant`/`DeleteTenant` against a real,
        ephemeral API server via `internal/testutil`, and asserts the real
        behavior the fake clientset can't (`kubernetes.io/metadata.name`
        injection, zero-drift diff after a full reconcile with no
        apiVersion/kind exception, and a `DeleteTenant` that leaves the
        namespace `Terminating` rather than vanishing synchronously, since
        envtest runs no namespace controller). Verified passing for real via
        `make envtest`, closing the project's last open item

## Resume point for next session

All four phases are complete. Phase 4's last open item,
`tests/envtest_test.go`, landed and passes for real (`make envtest`) against
a live ephemeral `etcd`/`kube-apiserver` pair — `CreateTenant`,
`UpdateTenant`, `ListTenants`, `GetTenant`, `DiffTenant`, and `DeleteTenant`
all exercised against a real control plane, not just the fake clientset.

The CLI has six commands (`generate`, `create`, `update`, `delete`, `list`,
`status`), cross-namespace NetworkPolicy allow-listing, `--dry-run`/`--diff`
modes, GitOps config-file specs via `--file`, live per-tenant quota usage
reporting via `status` and a `list` summary column, and a default-
`restricted` Pod Security Admission level per tenant (`--pod-security`).
Every command has been exercised for real against a local `kind` cluster,
and the full lifecycle has also been verified against a real `envtest` API
server with no Docker/kind dependency.

The only item not implemented is Helm chart packaging (Phase 3), which was
explicitly scoped and deferred — a single static binary plus
`generate | kubectl apply -f -` has covered every real need so far. It's
recorded as a future idea in DAILY_REPORT.md, not a gap in the current
scope.

If resuming this project further: Helm/kustomize packaging, a
`tenantctl doctor` drift-check command, or per-tenant egress allow-lists to
external CIDRs are the natural next features — see DAILY_REPORT.md's
"Future ideas" for the full list.

STATUS: COMPLETE
