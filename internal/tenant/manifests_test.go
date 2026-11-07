package tenant

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestBuild_Namespace(t *testing.T) {
	s := validSpec()
	b := Build(s)

	ns := b.Namespace
	if ns.Name != "acme-corp" {
		t.Errorf("namespace name = %q, want %q", ns.Name, "acme-corp")
	}
	if ns.Namespace != "" {
		t.Errorf("namespace object itself should have no namespace, got %q", ns.Namespace)
	}
	if ns.Labels[TenantLabelKey] != "acme-corp" {
		t.Errorf("namespace missing tenant label, got labels %v", ns.Labels)
	}
	if ns.Labels[TenantManagedByKey] != ManagedByValue {
		t.Errorf("namespace missing managed-by label, got labels %v", ns.Labels)
	}
}

func TestBuild_ResourceQuota(t *testing.T) {
	s := validSpec()
	s.Quota = Quota{
		CPURequest:    "1500m",
		MemoryRequest: "2Gi",
		CPULimit:      "3",
		MemoryLimit:   "6Gi",
		MaxPods:       "15",
	}
	b := Build(s)

	rq := b.ResourceQuota
	if rq.Name != "tenant-quota" {
		t.Errorf("resource quota name = %q, want %q", rq.Name, "tenant-quota")
	}
	if rq.Namespace != s.Name {
		t.Errorf("resource quota namespace = %q, want %q", rq.Namespace, s.Name)
	}
	if rq.Labels[TenantLabelKey] != s.Name {
		t.Errorf("resource quota missing tenant label")
	}

	wantHard := map[corev1.ResourceName]string{
		corev1.ResourceRequestsCPU:    "1500m",
		corev1.ResourceRequestsMemory: "2Gi",
		corev1.ResourceLimitsCPU:      "3",
		corev1.ResourceLimitsMemory:   "6Gi",
		corev1.ResourcePods:           "15",
	}
	for name, want := range wantHard {
		got, ok := rq.Spec.Hard[name]
		if !ok {
			t.Fatalf("resource quota missing hard limit for %q", name)
		}
		wantQty := resource.MustParse(want)
		if got.Cmp(wantQty) != 0 {
			t.Errorf("resource quota hard[%q] = %v, want %v", name, got.String(), want)
		}
	}
}

func TestBuild_LimitRange(t *testing.T) {
	s := validSpec()
	s.LimitRange = LimitRange{
		DefaultCPURequest:    "50m",
		DefaultMemoryRequest: "64Mi",
		DefaultCPULimit:      "250m",
		DefaultMemoryLimit:   "256Mi",
	}
	b := Build(s)

	lr := b.LimitRange
	if lr.Name != "tenant-limits" {
		t.Errorf("limit range name = %q, want %q", lr.Name, "tenant-limits")
	}
	if lr.Namespace != s.Name {
		t.Errorf("limit range namespace = %q, want %q", lr.Namespace, s.Name)
	}
	if len(lr.Spec.Limits) != 1 {
		t.Fatalf("expected exactly one LimitRangeItem, got %d", len(lr.Spec.Limits))
	}
	item := lr.Spec.Limits[0]
	if item.Type != corev1.LimitTypeContainer {
		t.Errorf("limit range item type = %q, want %q", item.Type, corev1.LimitTypeContainer)
	}

	checkQty := func(label string, list corev1.ResourceList, name corev1.ResourceName, want string) {
		got, ok := list[name]
		if !ok {
			t.Fatalf("limit range %s missing %q", label, name)
		}
		if got.Cmp(resource.MustParse(want)) != 0 {
			t.Errorf("limit range %s[%q] = %v, want %v", label, name, got.String(), want)
		}
	}
	checkQty("defaultRequest", item.DefaultRequest, corev1.ResourceCPU, "50m")
	checkQty("defaultRequest", item.DefaultRequest, corev1.ResourceMemory, "64Mi")
	checkQty("default", item.Default, corev1.ResourceCPU, "250m")
	checkQty("default", item.Default, corev1.ResourceMemory, "256Mi")
}

func findPolicy(policies []*networkingv1.NetworkPolicy, name string) *networkingv1.NetworkPolicy {
	for _, p := range policies {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func TestBuild_NetworkPolicies(t *testing.T) {
	s := validSpec()
	b := Build(s)

	if len(b.NetworkPolicies) != 3 {
		t.Fatalf("expected 3 network policies, got %d", len(b.NetworkPolicies))
	}

	for _, np := range b.NetworkPolicies {
		if np.Namespace != s.Name {
			t.Errorf("network policy %q namespace = %q, want %q", np.Name, np.Namespace, s.Name)
		}
		if np.Labels[TenantLabelKey] != s.Name {
			t.Errorf("network policy %q missing tenant label", np.Name)
		}
	}

	t.Run("deny-all blocks everything", func(t *testing.T) {
		np := findPolicy(b.NetworkPolicies, "deny-all")
		if np == nil {
			t.Fatal("deny-all policy not found")
		}
		if len(np.Spec.PolicyTypes) != 2 {
			t.Fatalf("deny-all should cover both ingress and egress, got %v", np.Spec.PolicyTypes)
		}
		if len(np.Spec.Ingress) != 0 || len(np.Spec.Egress) != 0 {
			t.Fatalf("deny-all should have no ingress/egress rules (true default-deny), got ingress=%v egress=%v", np.Spec.Ingress, np.Spec.Egress)
		}
		if len(np.Spec.PodSelector.MatchLabels) != 0 || len(np.Spec.PodSelector.MatchExpressions) != 0 {
			t.Fatalf("deny-all pod selector should select all pods (empty selector), got %+v", np.Spec.PodSelector)
		}
	})

	t.Run("allow-same-namespace permits intra-namespace traffic", func(t *testing.T) {
		np := findPolicy(b.NetworkPolicies, "allow-same-namespace")
		if np == nil {
			t.Fatal("allow-same-namespace policy not found")
		}
		if len(np.Spec.Ingress) != 1 || len(np.Spec.Ingress[0].From) != 1 {
			t.Fatalf("expected a single ingress rule allowing same-namespace peers, got %+v", np.Spec.Ingress)
		}
		from := np.Spec.Ingress[0].From[0]
		if from.NamespaceSelector != nil {
			t.Errorf("allow-same-namespace ingress peer should not set a namespace selector (so it stays scoped to the policy's own namespace), got %+v", from.NamespaceSelector)
		}
		if from.PodSelector == nil {
			t.Fatal("allow-same-namespace ingress peer should select pods via an empty pod selector")
		}

		if len(np.Spec.Egress) != 1 || len(np.Spec.Egress[0].To) != 1 {
			t.Fatalf("expected a single egress rule allowing same-namespace peers, got %+v", np.Spec.Egress)
		}
		to := np.Spec.Egress[0].To[0]
		if to.NamespaceSelector != nil {
			t.Errorf("allow-same-namespace egress peer should not set a namespace selector, got %+v", to.NamespaceSelector)
		}
	})

	t.Run("allow-dns-egress permits only kube-system DNS", func(t *testing.T) {
		np := findPolicy(b.NetworkPolicies, "allow-dns-egress")
		if np == nil {
			t.Fatal("allow-dns-egress policy not found")
		}
		if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
			t.Fatalf("allow-dns-egress should only cover egress, got %v", np.Spec.PolicyTypes)
		}
		if len(np.Spec.Ingress) != 0 {
			t.Fatalf("allow-dns-egress should not permit any ingress, got %v", np.Spec.Ingress)
		}
		if len(np.Spec.Egress) != 1 {
			t.Fatalf("expected exactly one egress rule, got %d", len(np.Spec.Egress))
		}
		egress := np.Spec.Egress[0]
		if len(egress.To) != 1 || egress.To[0].NamespaceSelector == nil {
			t.Fatalf("expected egress destined to a namespace selector, got %+v", egress.To)
		}
		if egress.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "kube-system" {
			t.Fatalf("expected DNS egress to target kube-system, got %+v", egress.To[0].NamespaceSelector.MatchLabels)
		}
		if len(egress.Ports) != 2 {
			t.Fatalf("expected UDP and TCP port 53, got %+v", egress.Ports)
		}
		for _, p := range egress.Ports {
			if p.Port == nil || p.Port.IntValue() != 53 {
				t.Errorf("expected port 53, got %v", p.Port)
			}
		}
	})
}

func TestBuild_Role_ExcludesIsolationResources(t *testing.T) {
	s := validSpec()
	b := Build(s)

	role := b.Role
	if role.Name != "tenant-admin" {
		t.Errorf("role name = %q, want %q", role.Name, "tenant-admin")
	}
	if role.Namespace != s.Name {
		t.Errorf("role namespace = %q, want %q", role.Namespace, s.Name)
	}

	forbidden := []string{"resourcequotas", "limitranges", "networkpolicies", "roles", "rolebindings"}
	for _, rule := range role.Rules {
		for _, resourceName := range rule.Resources {
			for _, f := range forbidden {
				if resourceName == f {
					t.Errorf("tenant-admin role must not grant access to %q (would let a tenant loosen its own isolation boundary)", f)
				}
			}
		}
	}

	// Sanity: it should still grant something, otherwise the exclusion check above is vacuous.
	if len(role.Rules) == 0 {
		t.Fatal("tenant-admin role has no rules at all")
	}
}

func TestBuild_RoleBinding_SubjectKinds(t *testing.T) {
	cases := []struct {
		name          string
		subject       AdminSubject
		wantAPIGroup  string
		wantNamespace string
	}{
		{
			name:          "User",
			subject:       AdminSubject{Kind: SubjectKindUser, Name: "alice"},
			wantAPIGroup:  "rbac.authorization.k8s.io",
			wantNamespace: "",
		},
		{
			name:          "Group",
			subject:       AdminSubject{Kind: SubjectKindGroup, Name: "acme-admins"},
			wantAPIGroup:  "rbac.authorization.k8s.io",
			wantNamespace: "",
		},
		{
			name:          "ServiceAccount",
			subject:       AdminSubject{Kind: SubjectKindServiceAccount, Name: "ci-bot", Namespace: "ci"},
			wantAPIGroup:  "",
			wantNamespace: "ci",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validSpec()
			s.AdminSubject = c.subject
			b := Build(s)

			rb := b.RoleBinding
			if rb.Name != "tenant-admin-binding" {
				t.Errorf("role binding name = %q, want %q", rb.Name, "tenant-admin-binding")
			}
			if len(rb.Subjects) != 1 {
				t.Fatalf("expected exactly one subject, got %d", len(rb.Subjects))
			}
			subj := rb.Subjects[0]
			if subj.Kind != string(c.subject.Kind) {
				t.Errorf("subject kind = %q, want %q", subj.Kind, c.subject.Kind)
			}
			if subj.Name != c.subject.Name {
				t.Errorf("subject name = %q, want %q", subj.Name, c.subject.Name)
			}
			if subj.APIGroup != c.wantAPIGroup {
				t.Errorf("subject apiGroup = %q, want %q", subj.APIGroup, c.wantAPIGroup)
			}
			if subj.Namespace != c.wantNamespace {
				t.Errorf("subject namespace = %q, want %q", subj.Namespace, c.wantNamespace)
			}

			if rb.RoleRef.Kind != "Role" || rb.RoleRef.Name != "tenant-admin" || rb.RoleRef.APIGroup != "rbac.authorization.k8s.io" {
				t.Errorf("roleRef = %+v, want Role/tenant-admin/rbac.authorization.k8s.io", rb.RoleRef)
			}
		})
	}
}

func TestBuild_IsPureAndDeterministic(t *testing.T) {
	s := validSpec()
	b1 := Build(s)
	b2 := Build(s)

	y1, err := RenderYAML(b1)
	if err != nil {
		t.Fatalf("render b1: %v", err)
	}
	y2, err := RenderYAML(b2)
	if err != nil {
		t.Fatalf("render b2: %v", err)
	}
	if string(y1) != string(y2) {
		t.Fatal("Build should be a pure function of Spec: two builds of the same spec rendered differently")
	}
}
