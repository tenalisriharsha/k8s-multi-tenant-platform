# Verification

This records a first-time-user run of this project from a fresh `git
clone`, in a throwaway directory outside the working copy, on 2026-11-12.
Environment: Go 1.27.1 (darwin/arm64), Docker 29.9.3, kind v0.33.0,
kubectl v1.36.1 client.

## First-use check

Followed the README "Quick start" section command by command, with no
fixes applied along the way.

```sh
git clone https://github.com/tenalisriharsha/k8s-multi-tenant-platform.git
cd k8s-multi-tenant-platform
go build -o bin/tenantctl ./cmd/tenantctl
./bin/tenantctl generate --name acme-corp --admin-name acme-admins
```

Expected: the build succeeds, and `generate` renders the full tenant
manifest bundle (namespace, quota, limit range, 3 network policies, role,
role binding) to stdout with no cluster contacted.

Observed: build exited 0 with no output. `generate` printed exactly that
bundle — a `Namespace` labeled `restricted` Pod Security Admission plus the
tenant labels, a `ResourceQuota` with the documented defaults (`cpu: 2`,
`memory: 4Gi` requests, `cpu: 4`, `memory: 8Gi` limits, `pods: 20`), a
`LimitRange` with the documented per-container defaults, `deny-all` /
`allow-same-namespace` / `allow-dns-egress` `NetworkPolicy` objects, and a
`Role`/`RoleBinding` scoped to the documented resource list. Matches the
README's "What gets created" section exactly.

Continued with the real-cluster portion of Quick start:

```sh
kind create cluster --name tenant-demo
./bin/tenantctl create --name acme-corp --admin-name acme-admins --context kind-tenant-demo
./bin/tenantctl list --context kind-tenant-demo
./bin/tenantctl status acme-corp --context kind-tenant-demo
./bin/tenantctl update --name acme-corp --admin-name acme-admins --cpu-request 3 --context kind-tenant-demo
./bin/tenantctl delete --name acme-corp --context kind-tenant-demo
```

Expected: a kind cluster comes up; `create` applies the bundle and reports
success; `list` shows the tenant active with a quota summary; `status`
shows per-resource usage at 0%; `update` reconciles the raised CPU request;
`delete` removes the tenant.

Observed: every command matched exactly. `create` printed `tenant
"acme-corp" created: namespace (pod-security=restricted), quota, limit
range, 3 network policies, and RBAC applied`. `list` showed
`acme-corp  Active  <timestamp>  cpu 0%`. `status` showed all five tracked
resources (`requests.cpu`, `requests.memory`, `limits.cpu`,
`limits.memory`, `pods`) at `0  <hard>  0%`. `update` printed the
documented reconciliation message, and a direct `kubectl get
resourcequota` confirmed `requests.cpu` was live-updated to `3`. `delete`
printed `tenant "acme-corp" deleted`.

No undocumented step was needed anywhere in this flow.

Result: PASS

## Headline feature run

The project's main feature is `tenantctl` onboarding a tenant end to end
against a real cluster — namespace, quota, limit range, deny-by-default
network isolation, Pod Security Admission, and scoped RBAC, applied as one
bundle. Ran this for real via the project's own documented end-to-end
script, [`scripts/demo.sh`](../scripts/demo.sh), against the same kind
cluster used above — no mocking of any part of `tenantctl` itself.

```sh
./scripts/demo.sh tenant-demo
```

Expected (per the script and README): build the binary, create tenant
`demo-corp`, show its namespace is labeled with the `restricted` Pod
Security Admission level, attempt to run a privileged pod in it and have
the cluster's admission controller reject it, list the tenant, show the
live quota/limitrange/networkpolicy/RBAC objects, show live quota usage,
raise the CPU quota and add a cross-namespace allow for `ingress-nginx`,
diff a further quota change without applying it, then delete the tenant.

Observed: every step matched. Notably, the real Kubernetes API server
rejected the privileged pod with:

```
Error from server (Forbidden): pods "privileged-test" is forbidden: violates PodSecurity "restricted:latest": privileged (container "privileged-test" must not set securityContext.privileged=true), allowPrivilegeEscalation != false (...), unrestricted capabilities (...), runAsNonRoot != true (...), seccompProfile (...)
```

— this is the real built-in Pod Security Admission controller refusing
the pod, exactly as the README's "What gets created" and "Troubleshooting"
sections describe, with no policy engine installed by the project. The
quota raise was confirmed live (`requests.cpu` went from `2` to `3`,
visible in the subsequent `networkpolicy` list growing from 3 to 4 objects
after `--allow-namespace ingress-nginx`), and `update --diff` printed a
correct line-level diff (`-     requests.cpu: "3"` / `+     requests.cpu:
"5"`) without applying the change — confirmed by the live object still
reading `3` afterward. `delete` removed the tenant namespace, observed
transitioning to `Terminating`.

The kind cluster was torn down afterward (`kind delete cluster --name
tenant-demo`); `kind get clusters` and `docker ps -a` confirm no leftover
cluster or containers from this run.

This project does not call any LLM or external paid service, so the
"real model" requirement does not apply — confirmed by grepping the
source tree for any Claude/Anthropic/OpenAI/LLM reference, which found
none outside this verification note.

Result: PASS

## Test suite with development tools

Installed/used the exact tools the project's own CI
(`.github/workflows/ci.yml`) and `Makefile` document: Go 1.27 toolchain
(`go build`, `go vet`, `go test`, `gofmt`), and `setup-envtest` (pulled
automatically by `make envtest` via `go run`). The project has no separate
linter config (no `.golangci.yml` and `golangci-lint` is not installed),
so CI's own tool set is the full documented set.

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./... -cover
go test ./... -cover -race
make envtest
go test -tags e2e ./tests/... -v -timeout 5m
```

Expected: build, vet, and gofmt report nothing; the unit suite
(`cmd/tenantctl`, `internal/tenant`) passes; `make envtest` downloads the
kubebuilder `etcd`/`kube-apiserver` binaries on first run and passes the
`tests` package against a real ephemeral API server; the `e2e`-tagged
suite in `tests/e2e_kind_test.go` passes against a real `kind` cluster.

Observed: all of the above, exactly.

- `go build ./...` — exit 0, no output
- `go vet ./...` — exit 0, no output
- `gofmt -l .` — exit 0, no files listed
- `go test ./... -cover` — `cmd/tenantctl` 82.9%, `internal/tenant` 87.5%,
  both `ok`
- `go test ./... -cover -race` — same packages, same coverage, `ok`
- `make envtest` — downloaded the kubebuilder 1.33.0 assets, then `ok` on
  all three packages including `tests` (10.6s against a live control
  plane)
- `go test -tags e2e ./tests/... -v -timeout 5m` — `TestTenantctlAgainstRealCluster`
  and all six of its subtests (`create` applies the bundle, Pod Security
  Admission rejects a privileged pod and `update --pod-security baseline`
  allows one, `create` refuses a duplicate namespace, `list` shows the
  tenant, `delete` refuses an unmanaged namespace, `delete` removes the
  tenant) — all PASS in 42.3s

No build, vet, format, or test failure anywhere, with or without `-race`.

Result: PASS
