package tenant

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// The namespace itself is never deleted or recreated by update (a
	// tenant's name is immutable) -- only its labels can change, and this
	// spec's PodSecurityLevel didn't, so its labels must be untouched too.
	ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace should still exist after update: %v", err)
	}
	if ns.Labels[TenantLabelKey] != spec.Name || ns.Labels[TenantManagedByKey] != ManagedByValue {
		t.Errorf("namespace labels = %v, want unchanged tenant-identity labels", ns.Labels)
	}
}

// TestUpdateTenant_ReconcilesNamespacePodSecurityLabel pins down the Phase 4
// fix: before it, UpdateTenant's doc comment explicitly said the namespace
// was never touched at all, which meant a PodSecurityLevel change could
// never actually be applied by `update`. Now it must reconcile the
// namespace's pod-security.kubernetes.io/* labels to match the new spec.
func TestUpdateTenant_ReconcilesNamespacePodSecurityLabel(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "psa-corp"
	spec.PodSecurityLevel = PodSecurityRestricted

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	lowered := spec
	lowered.PodSecurityLevel = PodSecurityBaseline
	if err := client.UpdateTenant(ctx, Build(lowered)); err != nil {
		t.Fatalf("UpdateTenant (lower PSA level): %v", err)
	}

	ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	for _, key := range []string{
		"pod-security.kubernetes.io/enforce",
		"pod-security.kubernetes.io/audit",
		"pod-security.kubernetes.io/warn",
	} {
		if got := ns.Labels[key]; got != "baseline" {
			t.Errorf("namespace label %q = %q, want %q after update", key, got, "baseline")
		}
	}
	if ns.Name != spec.Name {
		t.Errorf("namespace name changed to %q, want it to remain %q (a tenant can't be renamed by update)", ns.Name, spec.Name)
	}

	disabled := spec
	disabled.PodSecurityLevel = ""
	if err := client.UpdateTenant(ctx, Build(disabled)); err != nil {
		t.Fatalf("UpdateTenant (disable PSA): %v", err)
	}
	ns, err = kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace after disabling PSA: %v", err)
	}
	for _, key := range []string{
		"pod-security.kubernetes.io/enforce",
		"pod-security.kubernetes.io/audit",
		"pod-security.kubernetes.io/warn",
	} {
		if _, ok := ns.Labels[key]; ok {
			t.Errorf("expected label %q to be removed once PodSecurityLevel is cleared, got %v", key, ns.Labels)
		}
	}
	if ns.Labels[TenantManagedByKey] != ManagedByValue {
		t.Errorf("expected tenant-identity labels to survive clearing PSA labels, got %v", ns.Labels)
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

func TestDiffTenant_ShowsPodSecurityLabelDrift(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "psa-diff-corp"
	spec.PodSecurityLevel = PodSecurityRestricted

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	lowered := spec
	lowered.PodSecurityLevel = PodSecurityBaseline

	diff, err := client.DiffTenant(ctx, Build(lowered))
	if err != nil {
		t.Fatalf("DiffTenant: %v", err)
	}

	var hasRemoved, hasAdded bool
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "pod-security.kubernetes.io/enforce: restricted") {
			hasRemoved = true
		}
		if strings.HasPrefix(line, "+ ") && strings.Contains(line, "pod-security.kubernetes.io/enforce: baseline") {
			hasAdded = true
		}
	}
	if !hasRemoved || !hasAdded {
		t.Errorf("expected diff to show the PodSecurityLevel changing from restricted to baseline, got:\n%s", diff)
	}
}

// TestDiffTenant_StripsAPIServerInjectedMetadataNameLabel pins down a real
// gap found against a live kind cluster: a real API server stamps every
// namespace with a kubernetes.io/metadata.name label that Build() never
// sets, which would otherwise show up as permanent diff noise on every
// tenant, forever. The fake clientset used elsewhere in this file never
// injects that label on its own, so this test adds it by hand to the seeded
// object to reproduce the real-cluster shape DiffTenant has to handle.
func TestDiffTenant_StripsAPIServerInjectedMetadataNameLabel(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "metadata-name-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	ns, err := kube.CoreV1().Namespaces().Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get namespace: %v", err)
	}
	ns.Labels["kubernetes.io/metadata.name"] = spec.Name
	if _, err := kube.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("seed kubernetes.io/metadata.name label: %v", err)
	}

	diff, err := client.DiffTenant(ctx, Build(spec))
	if err != nil {
		t.Fatalf("DiffTenant: %v", err)
	}
	// The DNS-egress NetworkPolicy legitimately uses the
	// "kubernetes.io/metadata.name: kube-system" selector label elsewhere in
	// the diff, so match on the tenant's own name specifically rather than
	// the bare key, to isolate the namespace's own injected label.
	noise := "kubernetes.io/metadata.name: " + spec.Name
	if strings.Contains(diff, noise) {
		t.Errorf("expected %q to be stripped from the diff as API-server noise, got:\n%s", noise, diff)
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

func TestResourceUsage_Percent(t *testing.T) {
	cases := []struct {
		name string
		used string
		hard string
		want int
	}{
		{"half used", "1", "2", 50},
		{"zero hard avoids divide by zero", "1", "0", 0},
		{"used equals hard", "4Gi", "4Gi", 100},
		{"zero used", "0", "4", 0},
		{"rounds down", "1", "3", 33},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := ResourceUsage{
				Used: resource.MustParse(c.used),
				Hard: resource.MustParse(c.hard),
			}
			if got := u.Percent(); got != c.want {
				t.Errorf("Percent() with used=%s hard=%s = %d, want %d", c.used, c.hard, got, c.want)
			}
		})
	}
}

// setQuotaStatus fetches ns's tenant-quota ResourceQuota and overwrites its
// Status to simulate what Kubernetes' own quota controller would compute,
// since the fake clientset (unlike a real cluster) never fills this in on
// its own.
func setQuotaStatus(t *testing.T, kube *fake.Clientset, ns string, hard, used corev1.ResourceList) {
	t.Helper()
	ctx := context.Background()
	rq, err := kube.CoreV1().ResourceQuotas(ns).Get(ctx, "tenant-quota", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get resource quota to seed status: %v", err)
	}
	rq.Status = corev1.ResourceQuotaStatus{Hard: hard, Used: used}
	if _, err := kube.CoreV1().ResourceQuotas(ns).Update(ctx, rq, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("seed resource quota status: %v", err)
	}
}

func TestGetTenant_ReturnsUsageFromResourceQuotaStatus(t *testing.T) {
	ctx := context.Background()
	spec := validSpec()
	spec.Name = "status-corp"

	kube := fake.NewSimpleClientset()
	client := NewClient(kube)
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	setQuotaStatus(t, kube, spec.Name,
		corev1.ResourceList{
			corev1.ResourceRequestsCPU: resource.MustParse("2"),
			corev1.ResourcePods:        resource.MustParse("20"),
		},
		corev1.ResourceList{
			corev1.ResourceRequestsCPU: resource.MustParse("1"),
			corev1.ResourcePods:        resource.MustParse("5"),
		},
	)

	info, err := client.GetTenant(ctx, spec.Name)
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if info.Name != spec.Name {
		t.Errorf("Info.Name = %q, want %q", info.Name, spec.Name)
	}
	if len(info.Usage) != 2 {
		t.Fatalf("expected usage for exactly the 2 hard-limited resources, got %+v", info.Usage)
	}
	byResource := make(map[corev1.ResourceName]ResourceUsage, len(info.Usage))
	for _, u := range info.Usage {
		byResource[u.Resource] = u
	}
	if u := byResource[corev1.ResourceRequestsCPU]; u.Percent() != 50 {
		t.Errorf("requests.cpu usage = %+v, want 50%%", u)
	}
	if u := byResource[corev1.ResourcePods]; u.Percent() != 25 {
		t.Errorf("pods usage = %+v, want 25%%", u)
	}
}

func TestGetTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	_, err := client.GetTenant(ctx, "ghost-tenant")
	if err == nil {
		t.Fatal("expected error getting a tenant that does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected a 'not found' error, got: %v", err)
	}
}

func TestGetTenant_RefusesUnmanagedNamespace(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	if _, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "someones-namespace"},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	_, err := client.GetTenant(ctx, "someones-namespace")
	if err == nil {
		t.Fatal("expected GetTenant to refuse an unmanaged namespace")
	}
	if !strings.Contains(err.Error(), "not managed by tenantctl") {
		t.Errorf("expected a 'not managed by tenantctl' error, got: %v", err)
	}
}

// TestGetTenant_NoQuotaYetReturnsNilUsageNotError pins down the documented
// behavior (PROGRESS.md: "a missing ResourceQuota ... is treated as 'no
// usage data yet,' not an error") for a managed namespace whose
// ResourceQuota hasn't been created yet, e.g. mid-creation.
func TestGetTenant_NoQuotaYetReturnsNilUsageNotError(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	if _, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "half-created-corp",
			Labels: map[string]string{TenantLabelKey: "half-created-corp", TenantManagedByKey: ManagedByValue},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}

	info, err := client.GetTenant(ctx, "half-created-corp")
	if err != nil {
		t.Fatalf("GetTenant should not error when the ResourceQuota doesn't exist yet: %v", err)
	}
	if info.Usage != nil {
		t.Errorf("expected nil Usage when no ResourceQuota exists, got %+v", info.Usage)
	}
}

func TestListTenants_IncludesUsagePerTenant(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	spec := validSpec()
	spec.Name = "list-usage-corp"
	if err := client.CreateTenant(ctx, Build(spec)); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	setQuotaStatus(t, kube, spec.Name,
		corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("2")},
		corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")},
	)

	tenants, err := client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("expected a single tenant, got %+v", tenants)
	}
	if len(tenants[0].Usage) != 1 || tenants[0].Usage[0].Percent() != 50 {
		t.Errorf("expected ListTenants to include live usage (50%%), got %+v", tenants[0].Usage)
	}
}

// TestListTenants_MissingQuotaForOneTenantDoesNotFailList pins down the
// documented behavior: a tenant whose ResourceQuota is briefly missing
// (e.g. still being created) must not fail list/status for every other
// tenant.
func TestListTenants_MissingQuotaForOneTenantDoesNotFailList(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	client := NewClient(kube)

	ready := validSpec()
	ready.Name = "ready-corp"
	if err := client.CreateTenant(ctx, Build(ready)); err != nil {
		t.Fatalf("CreateTenant(ready): %v", err)
	}

	gapped := validSpec()
	gapped.Name = "gapped-corp"
	if err := client.CreateTenant(ctx, Build(gapped)); err != nil {
		t.Fatalf("CreateTenant(gapped): %v", err)
	}
	if err := kube.CoreV1().ResourceQuotas(gapped.Name).Delete(ctx, "tenant-quota", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete resource quota to simulate a mid-creation gap: %v", err)
	}

	tenants, err := client.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants should succeed even when one tenant's quota is missing: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected both tenants to still be listed, got %+v", tenants)
	}

	byName := make(map[string]Info, len(tenants))
	for _, info := range tenants {
		byName[info.Name] = info
	}
	if byName["gapped-corp"].Usage != nil {
		t.Errorf("expected nil usage for the tenant with no ResourceQuota, got %+v", byName["gapped-corp"].Usage)
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
