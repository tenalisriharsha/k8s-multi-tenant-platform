//go:build envtest

// Package integration exercises internal/tenant.Client against a real (if
// ephemeral) Kubernetes API server started by internal/testutil's envtest
// bootstrap, rather than the k8s.io/client-go/kubernetes/fake used by
// internal/tenant's own unit tests. No part of tenantctl's own code
// (tenant.Build, tenant.Validate, tenant.Client) is mocked here; only the
// cluster is real infrastructure spun up for the test.
//
// This is gated behind the "envtest" build tag, same as
// .github/workflows/ci.yml and `make envtest`, and requires kubebuilder's
// etcd/kube-apiserver test binaries on disk (see
// sigs.k8s.io/controller-runtime/tools/setup-envtest). Run it with:
//
//	make envtest
package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/testutil"
)

// mustStartEnvtest starts a real ephemeral API server via internal/testutil
// and returns a typed clientset for it, registering cleanup to stop the
// server when the test finishes. It skips (rather than fails) when
// KUBEBUILDER_ASSETS isn't set, since that means the kubebuilder test
// binaries this needs aren't wired up in the current shell -- `make
// envtest` and CI both set it before running this suite.
func mustStartEnvtest(t *testing.T) kubernetes.Interface {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make envtest` rather than `go test -tags envtest` directly")
	}

	env := testutil.NewEnvtestEnvironment()
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest environment: %v", err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Logf("stop envtest environment: %v", err)
		}
	})

	kube, err := testutil.NewClientset(cfg)
	if err != nil {
		t.Fatalf("testutil.NewClientset: %v", err)
	}
	return kube
}

// TestEnvtestInfrastructure_StartsRealAPIServer pins down the Night 5
// infrastructure itself, independent of tenant.Client: a real, ephemeral
// control plane comes up, a typed clientset can talk to it, and that API
// server behaves like a real one rather than client-go's fake -- in
// particular, it injects the kubernetes.io/metadata.name label on every
// namespace (which the fake clientset never does, per internal/tenant's own
// DiffTenant tests) and never echoes back a created object's TypeMeta on a
// later Get (which the fake clientset does, also noted in those tests).
func TestEnvtestInfrastructure_StartsRealAPIServer(t *testing.T) {
	kube := mustStartEnvtest(t)
	ctx := context.Background()

	const name = "envtest-infra-smoke"
	created, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create namespace against envtest: %v", err)
	}
	if created.TypeMeta != (metav1.TypeMeta{}) {
		t.Errorf("Create response TypeMeta = %+v, want zero value (typed client-go clears it even on Create)", created.TypeMeta)
	}

	got, err := kube.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace back from envtest: %v", err)
	}
	if got.TypeMeta != (metav1.TypeMeta{}) {
		t.Errorf("Get response TypeMeta = %+v, want zero value", got.TypeMeta)
	}
	if got.Labels["kubernetes.io/metadata.name"] != name {
		t.Errorf("expected a real API server to inject kubernetes.io/metadata.name=%q, got labels %v", name, got.Labels)
	}
	if got.Status.Phase != corev1.NamespaceActive {
		t.Errorf("namespace phase = %q, want Active immediately after create", got.Status.Phase)
	}
}

// diffDriftLines returns only the changed ("- "/"+ ") lines of a DiffTenant
// result, ignoring the unchanged "  " lines diffLines always includes.
func diffDriftLines(diff string) []string {
	var drift []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "+ ") {
			drift = append(drift, line)
		}
	}
	return drift
}

func lifecycleSpec() tenant.Spec {
	return tenant.Spec{
		Name:              "envtest-corp",
		Quota:             tenant.DefaultQuota(),
		LimitRange:        tenant.DefaultLimitRange(),
		AllowedNamespaces: []string{"shared-ingress"},
		PodSecurityLevel:  tenant.PodSecurityRestricted,
		AdminSubject: tenant.AdminSubject{
			Kind: tenant.SubjectKindGroup,
			Name: "envtest-admins",
		},
	}
}

// TestTenantLifecycle_AgainstRealAPIServer drives the full documented
// tenantctl workflow (create -> diff -> list/get -> update -> diff ->
// delete) through tenant.Client against a real envtest API server. It is
// the real-cluster counterpart to internal/tenant/client_test.go's
// fake-clientset tests, and specifically checks the places those tests had
// to work around fake-clientset-only quirks (excluding apiVersion/kind from
// the "no drift" diff assertion) -- against a real server, no such
// exclusion should be necessary.
func TestTenantLifecycle_AgainstRealAPIServer(t *testing.T) {
	kube := mustStartEnvtest(t)
	ctx := context.Background()
	client := tenant.NewClient(kube)

	spec := lifecycleSpec()
	if err := tenant.Validate(spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	t.Run("create applies the full bundle with no drift against the real API server", func(t *testing.T) {
		if err := client.CreateTenant(ctx, tenant.Build(spec)); err != nil {
			t.Fatalf("CreateTenant: %v", err)
		}

		ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("namespace not created: %v", err)
		}
		if ns.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
			t.Errorf("expected restricted PodSecurity label, got %v", ns.Labels)
		}
		if ns.Labels["kubernetes.io/metadata.name"] != spec.Name {
			t.Errorf("expected real API server to inject kubernetes.io/metadata.name, got %v", ns.Labels)
		}

		if _, err := kube.CoreV1().ResourceQuotas(spec.Name).Get(ctx, tenant.ResourceQuotaName, metav1.GetOptions{}); err != nil {
			t.Errorf("resource quota not created: %v", err)
		}
		if _, err := kube.CoreV1().LimitRanges(spec.Name).Get(ctx, "tenant-limits", metav1.GetOptions{}); err != nil {
			t.Errorf("limit range not created: %v", err)
		}
		for _, np := range []string{"deny-all", "allow-same-namespace", "allow-dns-egress", "allow-cross-namespace"} {
			if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, np, metav1.GetOptions{}); err != nil {
				t.Errorf("network policy %q not created: %v", np, err)
			}
		}
		if _, err := kube.RbacV1().Roles(spec.Name).Get(ctx, "tenant-admin", metav1.GetOptions{}); err != nil {
			t.Errorf("role not created: %v", err)
		}
		if _, err := kube.RbacV1().RoleBindings(spec.Name).Get(ctx, "tenant-admin-binding", metav1.GetOptions{}); err != nil {
			t.Errorf("role binding not created: %v", err)
		}

		diff, err := client.DiffTenant(ctx, tenant.Build(spec))
		if err != nil {
			t.Fatalf("DiffTenant: %v", err)
		}
		if drift := diffDriftLines(diff); len(drift) != 0 {
			t.Errorf("expected zero drift immediately after create against a real API server (no apiVersion/kind exception needed, unlike the fake clientset), got:\n%s\nfull diff:\n%s", strings.Join(drift, "\n"), diff)
		}
	})

	t.Run("list and get reflect the created tenant", func(t *testing.T) {
		tenants, err := client.ListTenants(ctx)
		if err != nil {
			t.Fatalf("ListTenants: %v", err)
		}
		if len(tenants) != 1 || tenants[0].Name != spec.Name {
			t.Fatalf("ListTenants = %+v, want a single entry named %q", tenants, spec.Name)
		}
		if tenants[0].Phase != string(corev1.NamespaceActive) {
			t.Errorf("Phase = %q, want Active", tenants[0].Phase)
		}

		info, err := client.GetTenant(ctx, spec.Name)
		if err != nil {
			t.Fatalf("GetTenant: %v", err)
		}
		if info.Name != spec.Name {
			t.Errorf("GetTenant Name = %q, want %q", info.Name, spec.Name)
		}
		// envtest runs only etcd and kube-apiserver, not kube-controller-manager,
		// so no resourcequota controller ever populates ResourceQuota.Status
		// here -- unlike the fake-clientset tests, which seed Status by hand
		// (see client_test.go's setQuotaStatus), there is nothing to seed it on
		// a real API server in this environment. Usage must come back empty
		// rather than erroring.
		if len(info.Usage) != 0 {
			t.Errorf("Usage = %+v, want empty (no quota controller runs under envtest)", info.Usage)
		}
	})

	t.Run("update reconciles quota, PSA labels, and network policies with no drift after", func(t *testing.T) {
		updated := spec
		updated.Quota.CPURequest = "3"
		updated.PodSecurityLevel = tenant.PodSecurityBaseline
		updated.AllowedNamespaces = nil

		if err := client.UpdateTenant(ctx, tenant.Build(updated)); err != nil {
			t.Fatalf("UpdateTenant: %v", err)
		}

		rq, err := kube.CoreV1().ResourceQuotas(spec.Name).Get(ctx, tenant.ResourceQuotaName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get resource quota: %v", err)
		}
		if got := rq.Spec.Hard[corev1.ResourceRequestsCPU]; got.String() != "3" {
			t.Errorf("requests.cpu = %v, want 3 after update", got.String())
		}

		ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get namespace: %v", err)
		}
		if ns.Labels["pod-security.kubernetes.io/enforce"] != "baseline" {
			t.Errorf("expected PodSecurity label relabeled to baseline, got %v", ns.Labels)
		}
		if ns.Labels["kubernetes.io/metadata.name"] != spec.Name {
			t.Errorf("expected API-server-injected label to survive update untouched, got %v", ns.Labels)
		}

		if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, "allow-cross-namespace", metav1.GetOptions{}); err == nil {
			t.Error("expected allow-cross-namespace policy to be deleted after removing the allow-list, but it still exists")
		}

		diff, err := client.DiffTenant(ctx, tenant.Build(updated))
		if err != nil {
			t.Fatalf("DiffTenant after update: %v", err)
		}
		if drift := diffDriftLines(diff); len(drift) != 0 {
			t.Errorf("expected zero drift after update fully reconciled the real API server, got:\n%s\nfull diff:\n%s", strings.Join(drift, "\n"), diff)
		}

		spec = updated
	})

	t.Run("delete is accepted but the real API server leaves the namespace Terminating without a controller-manager", func(t *testing.T) {
		if err := client.DeleteTenant(ctx, spec.Name); err != nil {
			t.Fatalf("DeleteTenant: %v", err)
		}

		// Unlike the fake clientset (TestTenantLifecycle_EndToEnd in
		// client_test.go, which deletes synchronously), a real API server's
		// namespace deletion is finalized by the namespace controller inside
		// kube-controller-manager. envtest runs only etcd and kube-apiserver,
		// so that finalizer is never removed here: the namespace must still
		// exist, now Terminating, rather than vanishing.
		ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected namespace to still exist pending finalization, Get failed: %v", err)
		}
		if ns.DeletionTimestamp == nil {
			t.Error("expected DeletionTimestamp to be set after DeleteTenant")
		}
		if ns.Status.Phase != corev1.NamespaceTerminating {
			t.Errorf("namespace phase = %q, want Terminating", ns.Status.Phase)
		}
	})
}
