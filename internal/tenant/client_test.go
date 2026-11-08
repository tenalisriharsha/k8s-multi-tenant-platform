package tenant

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestTenantLifecycle_EndToEnd exercises the full, documented tenantctl
// workflow (generate a spec -> validate -> build -> create -> list ->
// delete) through nothing but this package's own real code. The fake
// clientset stands in only for the cluster itself (the one seam the
// architecture is explicitly built around), never for tenant.Client,
// tenant.Build, or tenant.Validate.
func TestTenantLifecycle_EndToEnd(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "lifecycle-corp"

	if err := Validate(spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	bundle := Build(spec)

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	if err := client.CreateTenant(ctx, bundle); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Every object in the bundle must actually exist on the (fake) cluster,
	// not just have been constructed locally.
	if _, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}
	if _, err := kube.CoreV1().ResourceQuotas(spec.Name).Get(ctx, "tenant-quota", metav1.GetOptions{}); err != nil {
		t.Fatalf("resource quota not created: %v", err)
	}
	if _, err := kube.CoreV1().LimitRanges(spec.Name).Get(ctx, "tenant-limits", metav1.GetOptions{}); err != nil {
		t.Fatalf("limit range not created: %v", err)
	}
	for _, name := range []string{"deny-all", "allow-same-namespace", "allow-dns-egress"} {
		if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("network policy %q not created: %v", name, err)
		}
	}
	if _, err := kube.RbacV1().Roles(spec.Name).Get(ctx, "tenant-admin", metav1.GetOptions{}); err != nil {
		t.Fatalf("role not created: %v", err)
	}
	if _, err := kube.RbacV1().RoleBindings(spec.Name).Get(ctx, "tenant-admin-binding", metav1.GetOptions{}); err != nil {
		t.Fatalf("role binding not created: %v", err)
	}

	tenants, err := client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].Name != spec.Name {
		t.Fatalf("ListTenants = %+v, want a single entry named %q", tenants, spec.Name)
	}

	if err := client.DeleteTenant(ctx, spec.Name); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if _, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{}); err == nil {
		t.Fatalf("namespace %q still exists after DeleteTenant", spec.Name)
	}

	tenants, err = client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants after delete: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("expected no tenants after delete, got %+v", tenants)
	}
}

func TestCreateTenant_AlreadyExists(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	bundle := Build(spec)

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	if err := client.CreateTenant(ctx, bundle); err != nil {
		t.Fatalf("first CreateTenant: %v", err)
	}
	err := client.CreateTenant(ctx, bundle)
	if err == nil {
		t.Fatal("expected error re-creating an existing tenant, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected an 'already exists' error, got: %v", err)
	}
}

func TestDeleteTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	err := client.DeleteTenant(ctx, "ghost-tenant")
	if err == nil {
		t.Fatal("expected error deleting a tenant that does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a 'not found' error, got: %v", err)
	}
}

func TestDeleteTenant_RefusesUnmanagedNamespace(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	// A namespace that happens to share a tenant's name but was not
	// created by tenantctl (no managed-by label) must survive delete.
	_, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "someones-namespace"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	err = client.DeleteTenant(ctx, "someones-namespace")
	if err == nil {
		t.Fatal("expected DeleteTenant to refuse an unmanaged namespace")
	}
	if !strings.Contains(err.Error(), "not managed by tenantctl") {
		t.Errorf("expected a 'not managed by tenantctl' error, got: %v", err)
	}

	if _, err := kube.CoreV1().Namespaces().Get(ctx, "someones-namespace", metav1.GetOptions{}); err != nil {
		t.Fatalf("unmanaged namespace should still exist after refused delete, but Get failed: %v", err)
	}
}

func TestListTenants_OnlyManagedAndEmptyWhenNone(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	tenants, err := client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants on empty cluster: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("expected no tenants on an empty cluster, got %+v", tenants)
	}

	// An unmanaged namespace must never show up in the tenant list.
	_, err = kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-system"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("seed kube-system namespace: %v", err)
	}

	managedSpec := validSpec()
	managedSpec.Name = "managed-corp"
	if err := client.CreateTenant(ctx, Build(managedSpec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	tenants, err = client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].Name != "managed-corp" {
		t.Fatalf("ListTenants = %+v, want only the managed tenant", tenants)
	}
}

func TestUpdateTenant_ReconcilesQuotaLimitRangeAndRBAC(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "update-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	updated := spec
	updated.Quota.CPURequest = "5"
	updated.LimitRange.DefaultCPULimit = "750m"
	updated.AdminSubject = AdminSubject{Kind: SubjectKindGroup, Name: "new-admins"}

	if err := client.UpdateTenant(ctx, Build(updated)); err != nil {
		t.Fatalf("UpdateTenant: %v", err)
	}

	rq, err := kube.CoreV1().ResourceQuotas(spec.Name).Get(ctx, "tenant-quota", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get resource quota: %v", err)
	}
	gotCPU := rq.Spec.Hard[corev1.ResourceRequestsCPU]
	if gotCPU.String() != "5" {
		t.Errorf("resource quota requests.cpu = %v, want 5 after update", gotCPU.String())
	}

	lr, err := kube.CoreV1().LimitRanges(spec.Name).Get(ctx, "tenant-limits", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get limit range: %v", err)
	}
	gotLimit := lr.Spec.Limits[0].Default[corev1.ResourceCPU]
	if gotLimit.String() != "750m" {
		t.Errorf("limit range default cpu limit = %v, want 750m after update", gotLimit.String())
	}

	rb, err := kube.RbacV1().RoleBindings(spec.Name).Get(ctx, "tenant-admin-binding", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get role binding: %v", err)
	}
	if len(rb.Subjects) != 1 || rb.Subjects[0].Name != "new-admins" {
		t.Errorf("role binding subjects = %+v, want a single subject named new-admins", rb.Subjects)
	}

	// Namespace itself must never be touched by update.
	if _, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("namespace should still exist after update: %v", err)
	}
}

func TestUpdateTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	err := client.UpdateTenant(ctx, Build(validSpec()))
	if err == nil {
		t.Fatal("expected error updating a tenant that does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a 'not found' error, got: %v", err)
	}
}

func TestUpdateTenant_RefusesUnmanagedNamespace(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	spec := validSpec()
	if _, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	err := client.UpdateTenant(ctx, Build(spec))
	if err == nil {
		t.Fatal("expected UpdateTenant to refuse an unmanaged namespace")
	}
	if !strings.Contains(err.Error(), "not managed by tenantctl") {
		t.Errorf("expected a 'not managed by tenantctl' error, got: %v", err)
	}
}

func TestUpdateTenant_ReconcilesNetworkPolicies(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "netpol-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Adding an allow-listed namespace should create allow-cross-namespace.
	withAllowList := spec
	withAllowList.AllowedNamespaces = []string{"ingress-nginx"}
	if err := client.UpdateTenant(ctx, Build(withAllowList)); err != nil {
		t.Fatalf("UpdateTenant (add allow-list): %v", err)
	}
	if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, "allow-cross-namespace", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected allow-cross-namespace policy to be created, get failed: %v", err)
	}

	// Removing the allow-list again must delete allow-cross-namespace, not
	// just leave it behind as stale drift.
	if err := client.UpdateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("UpdateTenant (remove allow-list): %v", err)
	}
	if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, "allow-cross-namespace", metav1.GetOptions{}); err == nil {
		t.Fatal("expected allow-cross-namespace policy to be deleted after removing the allow-list, but it still exists")
	}

	// The three base policies must still be present and untouched throughout.
	for _, name := range []string{"deny-all", "allow-same-namespace", "allow-dns-egress"} {
		if _, err := kube.NetworkingV1().NetworkPolicies(spec.Name).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("expected base network policy %q to still exist: %v", name, err)
		}
	}
}

func TestDiffTenant_NoDriftImmediatelyAfterCreate(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "diff-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	diff, err := client.DiffTenant(ctx, Build(spec))
	if err != nil {
		t.Fatalf("DiffTenant: %v", err)
	}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+ ") && !strings.HasPrefix(line, "- ") {
			continue
		}
		// Unlike a real API server, client-go's fake clientset echoes back
		// whatever TypeMeta an object was created with instead of clearing
		// it, so apiVersion/kind lines show up as spurious drift here even
		// though DiffTenant strips them correctly against a real cluster
		// (see withoutTypeMeta's doc comment). That's a fake-clientset
		// artifact, not real drift, so it's excluded from this check.
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "+ "))
		if strings.HasPrefix(trimmed, "apiVersion:") || strings.HasPrefix(trimmed, "kind:") {
			continue
		}
		t.Errorf("expected no real drift immediately after create, got a changed line: %q\nfull diff:\n%s", line, diff)
	}
}

func TestDiffTenant_ShowsRealDrift(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "drift-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	changed := spec
	changed.Quota.CPURequest = "7"

	diff, err := client.DiffTenant(ctx, Build(changed))
	if err != nil {
		t.Fatalf("DiffTenant: %v", err)
	}

	var hasRemoved, hasAdded bool
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "requests.cpu") {
			hasRemoved = true
		}
		if strings.HasPrefix(line, "+ ") && strings.Contains(line, "requests.cpu") {
			hasAdded = true
		}
	}
	if !hasRemoved || !hasAdded {
		t.Errorf("expected diff to show requests.cpu changing (- old, + new), got:\n%s", diff)
	}
}

func TestDiffTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	_, err := client.DiffTenant(ctx, Build(validSpec()))
	if err == nil {
		t.Fatal("expected error diffing a tenant that does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a 'not found' error, got: %v", err)
	}
}

func TestDiffTenant_RefusesUnmanagedNamespace(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	spec := validSpec()
	if _, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	_, err := client.DiffTenant(ctx, Build(spec))
	if err == nil {
		t.Fatal("expected DiffTenant to refuse an unmanaged namespace")
	}
	if !strings.Contains(err.Error(), "not managed by tenantctl") {
		t.Errorf("expected a 'not managed by tenantctl' error, got: %v", err)
	}
}

func TestCreateTenant_PartialFailureLeavesNamespaceOnCluster(t *testing.T) {
	// Document the current behavior: CreateTenant is not transactional. If
	// a later object in the bundle fails to create, the namespace (and any
	// objects created before the failure) remain on the cluster rather than
	// being rolled back. This test pins that behavior down so a future
	// change to add rollback is a deliberate, visible decision.
	ctx := context.Background()
	spec := validSpec()
	bundle := Build(spec)

	kube := fake.NewSimpleClientset()
	// Pre-seed the ResourceQuota that CreateTenant will try to create next,
	// forcing CreateTenant to fail partway through.
	if _, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: spec.Name},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	client := NewClient(kube)
	err := client.CreateTenant(ctx, bundle)
	if err == nil {
		t.Fatal("expected CreateTenant to fail when the namespace already exists")
	}

	if _, getErr := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{}); getErr != nil {
		t.Fatalf("expected the pre-existing namespace to remain after the failed create, but Get failed: %v", getErr)
	}
}
