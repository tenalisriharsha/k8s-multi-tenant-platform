# k8s-multi-tenant-platform

[![CI](https://github.com/tenalisriharsha/k8s-multi-tenant-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/tenalisriharsha/k8s-multi-tenant-platform/actions/workflows/ci.yml)

A namespace-per-tenant multi-tenancy reference implementation for
Kubernetes, built entirely from upstream primitives — no service mesh, no
external policy engine. `tenantctl` is a CLI that onboards a new tenant end
to end: a dedicated namespace, a `ResourceQuota`, a `LimitRange`, deny-by-
default `NetworkPolicy` isolation, and scoped RBAC, applied as one
consistent bundle.

Why this matters: "give every team their own namespace" is easy to say and
easy to get wrong in the details — a missing default-deny policy, a quota
that doesn't actually bound memory limits, an RBAC role that lets a tenant
edit their own quota. This project pins down a working, tested version of
those details so it can be read end to end or dropped into a real cluster.

## Preview

| | |
|---|---|
| ![tenantctl command overview](docs/screenshots/01-help.png) | ![generate offline manifest bundle](docs/screenshots/02-generate.png) |
| `tenantctl --help` — the six-command CLI surface | `tenantctl generate` — the full manifest bundle, rendered offline |
| ![create onboarding a tenant on a real cluster](docs/screenshots/03-create-and-verify.png) | ![list tenants with live quota utilization](docs/screenshots/04-list.png) |
| `tenantctl create` onboarding `acme-corp` on a real `kind` cluster, verified with `kubectl` | `tenantctl list` — every tenant with a live quota-utilization summary |
| ![real NetworkPolicy isolation proof](docs/screenshots/05-network-isolation.png) | ![per-resource quota status across every tenant](docs/screenshots/12-status.png) |
| Live proof against real pod traffic: cross-namespace blocked, same-namespace and DNS allowed | `tenantctl status` — per-resource quota usage vs. limits, across every tenant |
| ![update reconciling quota and network allow-list](docs/screenshots/06-update-reconcile.png) | ![diff mode previewing drift](docs/screenshots/07-diff.png) |
| `tenantctl update` raising a quota and adding a cross-namespace allow-list to a live tenant | `tenantctl update --diff` — live cluster state vs. the desired spec, with only the actual drift shown |
| ![dry-run preview](docs/screenshots/08-dry-run.png) | ![GitOps config file](docs/screenshots/09-gitops-file.png) |
| `tenantctl create --dry-run` — preview the full bundle without touching the cluster | `tenantctl generate -f` — a tenant spec driven entirely from a YAML file |
| ![delete and safety guards](docs/screenshots/10-delete-and-safety.png) | ![input validation](docs/screenshots/11-validation.png) |
| `tenantctl delete`, plus the guard that refuses to delete a namespace it doesn't manage | Spec validation rejects a bad name or an unparsable resource quantity before touching the cluster |

## Quick start

Requires Go 1.27+.

```sh
git clone https://github.com/tenalisriharsha/k8s-multi-tenant-platform.git
cd k8s-multi-tenant-platform
go build -o bin/tenantctl ./cmd/tenantctl
./bin/tenantctl generate --name acme-corp --admin-name acme-admins
```

The command above renders the full tenant bundle to stdout — no cluster
required. To try it against a real cluster, point `tenantctl` at any
kubeconfig context, for example a local [kind](https://kind.sigs.k8s.io/)
cluster:

```sh
kind create cluster --name tenant-demo
./bin/tenantctl create --name acme-corp --admin-name acme-admins --context kind-tenant-demo
./bin/tenantctl list --context kind-tenant-demo
./bin/tenantctl status acme-corp --context kind-tenant-demo
./bin/tenantctl update --name acme-corp --admin-name acme-admins --cpu-request 3 --context kind-tenant-demo
./bin/tenantctl delete --name acme-corp --context kind-tenant-demo
```

Or run the same workflow end to end, including a real `kind` cluster
bring-up, with [`scripts/demo.sh`](scripts/demo.sh).

## What gets created

Running `create` for a tenant named `acme-corp` applies:

- **Namespace** `acme-corp`, labeled `tenant-platform.io/tenant=acme-corp`
  and `tenant-platform.io/managed-by=tenantctl`
- **ResourceQuota** `tenant-quota` capping total CPU/memory requests and
  limits and the max pod count in the namespace
- **LimitRange** `tenant-limits` setting default per-container CPU/memory
  requests and limits for pods that don't specify their own
- **NetworkPolicy** `deny-all`, `allow-same-namespace`, and
  `allow-dns-egress` — ingress and egress are denied by default, then
  explicitly re-allowed for traffic within the namespace and for DNS
  resolution against `kube-system`. A fourth policy, `allow-cross-namespace`,
  is added automatically whenever `--allow-namespace` is set (see below)
- **Role** `tenant-admin` and **RoleBinding** `tenant-admin-binding`,
  scoping the tenant's admin subject to workload resources (pods,
  deployments, jobs, services, configmaps, secrets, ...) in their own
  namespace. The role deliberately excludes `resourcequotas`,
  `limitranges`, `networkpolicies`, `roles`, and `rolebindings`, so a
  tenant admin can't loosen the isolation boundary `tenantctl` just put in
  place.

## Commands

### `tenantctl generate`

Renders a tenant's manifest bundle as a multi-document YAML stream, without
contacting a cluster. Useful for review, or for piping into
`kubectl apply -f -`.

```sh
tenantctl generate --name acme-corp --admin-name acme-admins [--output bundle.yaml]
```

### `tenantctl create`

Builds the same bundle and applies it to a cluster. Fails with a clear
error if the tenant's namespace already exists.

```sh
tenantctl create --name acme-corp --admin-name acme-admins [--kubeconfig path] [--context name]
```

Add `--dry-run` to print the bundle that would be applied and exit without
contacting the cluster at all.

### `tenantctl update`

Reconciles an existing tenant's quota, limit range, network policies, and
RBAC to match the given spec — the same flags (or `--file`) as `create`.
The tenant's namespace itself is never touched. Network policies are fully
reconciled: one that's no longer desired (e.g. a removed
`--allow-namespace`) is deleted, not just left behind. Fails if the tenant
doesn't exist or isn't `tenantctl`-managed.

```sh
tenantctl update --name acme-corp --admin-name acme-admins --cpu-request 3 --context kind-tenant-demo
```

Add `--dry-run` to render the desired bundle without contacting the
cluster, or `--diff` to fetch the tenant's live state and print a
line-level diff against the desired spec (lines only in the live cluster
prefixed `-`, lines only in the desired spec prefixed `+`) without applying
anything.

```sh
tenantctl update --name acme-corp --admin-name acme-admins --cpu-request 5 --diff --context kind-tenant-demo
```

### `tenantctl delete`

Deletes a tenant's namespace, cascading deletion of everything in it.
Refuses to act on any namespace not labeled as `tenantctl`-managed.

```sh
tenantctl delete --name acme-corp [--kubeconfig path] [--context name]
```

### `tenantctl list`

Lists every `tenantctl`-managed tenant namespace, with phase, creation
time, and a one-line quota summary (its single most-utilized resource, e.g.
`memory 72%`). For the full per-resource breakdown, use `status`.

```sh
tenantctl list [--kubeconfig path] [--context name]
```

### `tenantctl status`

Reports live `ResourceQuota` usage against hard limits, per resource, as a
percentage. With no argument, reports every tenant; with a tenant name,
reports just that tenant and fails if it doesn't exist or isn't
`tenantctl`-managed. This is read-only and complements, rather than
duplicates, `update --diff`: `status` is about quota usage numbers,
`update --diff` is about full-bundle spec-vs-live drift.

```sh
tenantctl status [name] [--kubeconfig path] [--context name]
```

### Shared flags

`generate`, `create`, and `update` all build a tenant spec from these flags
(shown with their defaults):

| Flag | Default | Meaning |
|---|---|---|
| `--file`, `-f` | | Load the full spec from a YAML file instead of the flags below (GitOps mode) |
| `--name` | *(required unless `--file`)* | Tenant name; must be a valid DNS label |
| `--cpu-request` | `2` | Total CPU requests allowed in the namespace |
| `--memory-request` | `4Gi` | Total memory requests allowed in the namespace |
| `--cpu-limit` | `4` | Total CPU limits allowed in the namespace |
| `--memory-limit` | `8Gi` | Total memory limits allowed in the namespace |
| `--max-pods` | `20` | Maximum pods allowed in the namespace |
| `--default-cpu-request` | `100m` | Per-container default CPU request |
| `--default-memory-request` | `128Mi` | Per-container default memory request |
| `--default-cpu-limit` | `500m` | Per-container default CPU limit |
| `--default-memory-limit` | `512Mi` | Per-container default memory limit |
| `--allow-namespace` | | Additional namespace allowed bidirectional NetworkPolicy traffic with this tenant (repeatable), e.g. a shared ingress controller namespace |
| `--admin-kind` | `Group` | RBAC subject kind: `User`, `Group`, or `ServiceAccount` |
| `--admin-name` | *(required unless `--file`)* | RBAC subject name bound as tenant admin |
| `--admin-namespace` | | Required when `--admin-kind=ServiceAccount` |

When `--file` is set, the other spec flags above are ignored. See
[`examples/tenant.yaml`](examples/tenant.yaml) for the file format — its
keys mirror the flags above (`quota.cpuRequest`, `limitRange.defaultCPULimit`,
`allowedNamespaces`, `adminSubject.kind`, ...).

`create`, `update`, `delete`, `list`, and `status` additionally accept
`--kubeconfig` and `--context` to target a specific cluster and context.

## Architecture

`internal/tenant` builds Kubernetes API objects from a plain `Spec` struct
(no cluster access), and applies them through a thin wrapper over
`kubernetes.Interface` so the apply/update/delete/list/diff logic can be
tested against a fake clientset. `cmd/tenantctl` wires that package to a
Cobra CLI. See [PROGRESS.md](PROGRESS.md) for the full design rationale and
the phased build plan.

```
cmd/tenantctl/       CLI entrypoint and subcommands
internal/tenant/     Spec validation, manifest builders, YAML rendering, cluster client
examples/            Sample GitOps tenant spec file for `--file`
scripts/             Local dev helpers: kind cluster bring-up, a full CLI demo, screenshot capture
docs/screenshots/    Preview images used above
```

## Development

```sh
go build ./...
go vet ./...
go test ./... -cover
```

Try the full workflow against a real cluster:

```sh
./scripts/kind-up.sh        # bring up (or reuse) a local kind cluster
./scripts/demo.sh           # build tenantctl and run create/list/update/diff/delete against it
```

## Status

Actively in development. [PROGRESS.md](PROGRESS.md) tracks the
architecture, the phased build plan, and exactly what's done versus planned
next.

## License

MIT — see [LICENSE](LICENSE).
