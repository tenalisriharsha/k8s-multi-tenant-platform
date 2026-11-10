package tenant

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ResourceQuotaName is the name every tenant's ResourceQuota object is
// built and looked up under. It's exported so client.go can fetch a
// tenant's live quota status without duplicating the literal.
const ResourceQuotaName = "tenant-quota"

// Bundle is the full set of Kubernetes objects that make up one tenant's
// isolation boundary.
type Bundle struct {
	Namespace       *corev1.Namespace
	ResourceQuota   *corev1.ResourceQuota
	LimitRange      *corev1.LimitRange
	NetworkPolicies []*networkingv1.NetworkPolicy
	Role            *rbacv1.Role
	RoleBinding     *rbacv1.RoleBinding
}

// Build renders a Spec into the full Bundle of objects tenantctl applies.
// It does not validate the Spec; call Validate first.
func Build(s Spec) Bundle {
	return Bundle{
		Namespace:       buildNamespace(s),
		ResourceQuota:   buildResourceQuota(s),
		LimitRange:      buildLimitRange(s),
		NetworkPolicies: buildNetworkPolicies(s),
		Role:            buildRole(s),
		RoleBinding:     buildRoleBinding(s),
	}
}

func tenantLabels(s Spec) map[string]string {
	return map[string]string{
		TenantLabelKey:     s.Name,
		TenantManagedByKey: ManagedByValue,
	}
}

func objectMeta(s Spec, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: s.Name,
		Labels:    tenantLabels(s),
	}
}

// podSecurityLabelKeys are the three standard Pod Security Admission labels.
// tenantctl always sets all three to the same level, so the audit and warn
// logs a namespace generates match the level it actually enforces.
var podSecurityLabelKeys = []string{
	"pod-security.kubernetes.io/enforce",
	"pod-security.kubernetes.io/audit",
	"pod-security.kubernetes.io/warn",
}

func buildNamespace(s Spec) *corev1.Namespace {
	labels := tenantLabels(s)
	if s.PodSecurityLevel != "" {
		for _, key := range podSecurityLabelKeys {
			labels[key] = string(s.PodSecurityLevel)
		}
	}

	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   s.Name,
			Labels: labels,
		},
	}
}

func mustQty(v string) resource.Quantity {
	q, err := resource.ParseQuantity(v)
	if err != nil {
		// Build is only ever called on a Spec that has passed Validate,
		// which already proved every quantity string parses.
		panic(err)
	}
	return q
}

func buildResourceQuota(s Spec) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ResourceQuota"},
		ObjectMeta: objectMeta(s, ResourceQuotaName),
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    mustQty(s.Quota.CPURequest),
				corev1.ResourceRequestsMemory: mustQty(s.Quota.MemoryRequest),
				corev1.ResourceLimitsCPU:      mustQty(s.Quota.CPULimit),
				corev1.ResourceLimitsMemory:   mustQty(s.Quota.MemoryLimit),
				corev1.ResourcePods:           mustQty(s.Quota.MaxPods),
			},
		},
	}
}

func buildLimitRange(s Spec) *corev1.LimitRange {
	return &corev1.LimitRange{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "LimitRange"},
		ObjectMeta: objectMeta(s, "tenant-limits"),
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    mustQty(s.LimitRange.DefaultCPURequest),
						corev1.ResourceMemory: mustQty(s.LimitRange.DefaultMemoryRequest),
					},
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    mustQty(s.LimitRange.DefaultCPULimit),
						corev1.ResourceMemory: mustQty(s.LimitRange.DefaultMemoryLimit),
					},
				},
			},
		},
	}
}

// buildNetworkPolicies returns three policies that together implement
// deny-by-default tenant network isolation:
//  1. deny-all: blocks all ingress and egress not otherwise allowed.
//  2. allow-same-namespace: permits traffic between pods in this namespace.
//  3. allow-dns-egress: permits egress to kube-system on port 53 so pods
//     can still resolve DNS despite the default egress deny.
func buildNetworkPolicies(s Spec) []*networkingv1.NetworkPolicy {
	denyAll := &networkingv1.NetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: objectMeta(s, "deny-all"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	allowSameNamespace := &networkingv1.NetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: objectMeta(s, "allow-same-namespace"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}},
		},
	}

	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	dnsPort := intstr.FromInt(53)
	allowDNSEgress := &networkingv1.NetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: objectMeta(s, "allow-dns-egress"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &udp, Port: &dnsPort},
					{Protocol: &tcp, Port: &dnsPort},
				},
			}},
		},
	}

	policies := []*networkingv1.NetworkPolicy{denyAll, allowSameNamespace, allowDNSEgress}
	if crossNS := buildCrossNamespacePolicy(s); crossNS != nil {
		policies = append(policies, crossNS)
	}
	return policies
}

// buildCrossNamespacePolicy returns a NetworkPolicy permitting bidirectional
// traffic between this tenant's namespace and every namespace listed in
// Spec.AllowedNamespaces (e.g. a shared ingress controller namespace). It
// returns nil when the tenant declares no allow-listed namespaces, so a
// tenant with an empty list gets exactly the same three policies as before
// this feature existed.
func buildCrossNamespacePolicy(s Spec) *networkingv1.NetworkPolicy {
	if len(s.AllowedNamespaces) == 0 {
		return nil
	}

	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(s.AllowedNamespaces))
	for _, ns := range s.AllowedNamespaces {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns},
			},
		})
	}

	return &networkingv1.NetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: objectMeta(s, "allow-cross-namespace"),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{From: peers}},
			Egress:  []networkingv1.NetworkPolicyEgressRule{{To: peers}},
		},
	}
}

// buildRole scopes a tenant admin to workload-related resources in their
// own namespace. It deliberately omits resourcequotas, limitranges,
// networkpolicies, roles, and rolebindings so a tenant admin cannot loosen
// the isolation boundary tenantctl just put in place.
func buildRole(s Spec) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "Role"},
		ObjectMeta: objectMeta(s, "tenant-admin"),
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{
					"pods", "pods/log", "pods/exec", "pods/portforward",
					"services", "endpoints", "configmaps", "secrets",
					"persistentvolumeclaims", "events", "serviceaccounts",
				},
				Verbs: []string{"*"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "replicasets", "statefulsets", "daemonsets"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs", "cronjobs"},
				Verbs:     []string{"*"},
			},
		},
	}
}

func buildRoleBinding(s Spec) *rbacv1.RoleBinding {
	subject := rbacv1.Subject{
		Kind: string(s.AdminSubject.Kind),
		Name: s.AdminSubject.Name,
	}
	if s.AdminSubject.Kind == SubjectKindServiceAccount {
		subject.Namespace = s.AdminSubject.Namespace
	}
	if s.AdminSubject.Kind == SubjectKindUser || s.AdminSubject.Kind == SubjectKindGroup {
		subject.APIGroup = "rbac.authorization.k8s.io"
	}

	return &rbacv1.RoleBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
		ObjectMeta: objectMeta(s, "tenant-admin-binding"),
		Subjects:   []rbacv1.Subject{subject},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "tenant-admin",
		},
	}
}
