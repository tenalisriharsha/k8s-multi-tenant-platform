// Package tenant builds and applies the Kubernetes objects that make up a
// single tenant's isolation boundary: a namespace, resource limits, network
// policy, and RBAC.
package tenant

// SubjectKind identifies the kind of RBAC subject granted admin access to a
// tenant's namespace.
type SubjectKind string

const (
	SubjectKindUser           SubjectKind = "User"
	SubjectKindGroup          SubjectKind = "Group"
	SubjectKindServiceAccount SubjectKind = "ServiceAccount"
)

// TenantLabelKey is applied, with the tenant name as its value, to every
// object tenantctl creates. TenantManagedByKey marks objects as owned by
// this tool. Both are used to select a tenant's objects for list/delete.
const (
	TenantLabelKey     = "tenant-platform.io/tenant"
	TenantManagedByKey = "tenant-platform.io/managed-by"
	ManagedByValue     = "tenantctl"
)

// Quota caps the aggregate resources a tenant's namespace may consume.
type Quota struct {
	CPURequest    string `json:"cpuRequest"`
	MemoryRequest string `json:"memoryRequest"`
	CPULimit      string `json:"cpuLimit"`
	MemoryLimit   string `json:"memoryLimit"`
	// MaxPods bounds the number of pods the namespace may run concurrently.
	MaxPods string `json:"maxPods"`
}

// LimitRange sets the per-container defaults and bounds applied when a pod
// in the tenant's namespace omits its own resource requests/limits.
type LimitRange struct {
	DefaultCPURequest    string `json:"defaultCPURequest"`
	DefaultMemoryRequest string `json:"defaultMemoryRequest"`
	DefaultCPULimit      string `json:"defaultCPULimit"`
	DefaultMemoryLimit   string `json:"defaultMemoryLimit"`
}

// AdminSubject is the RBAC subject bound to the tenant-admin Role in the
// tenant's namespace.
type AdminSubject struct {
	Kind      SubjectKind `json:"kind"`
	Name      string      `json:"name"`
	Namespace string      `json:"namespace,omitempty"` // required when Kind == SubjectKindServiceAccount
}

// PodSecurityLevel identifies a Kubernetes Pod Security Admission level.
type PodSecurityLevel string

const (
	PodSecurityPrivileged PodSecurityLevel = "privileged"
	PodSecurityBaseline   PodSecurityLevel = "baseline"
	PodSecurityRestricted PodSecurityLevel = "restricted"
)

// Spec is the declarative, cluster-independent description of a tenant.
// Everything tenantctl builds and applies is derived from a Spec.
type Spec struct {
	Name       string     `json:"name"`
	Quota      Quota      `json:"quota"`
	LimitRange LimitRange `json:"limitRange"`
	// AllowedNamespaces lists additional namespaces (matched by their
	// kubernetes.io/metadata.name label, same as the built-in DNS egress
	// rule) that this tenant's namespace may exchange traffic with in both
	// directions, e.g. a shared ingress controller namespace. Leave empty
	// for the default same-namespace-only isolation boundary.
	AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
	// PodSecurityLevel, when set to one of "privileged", "baseline", or
	// "restricted", applies that Pod Security Admission level to the
	// tenant's namespace via the standard pod-security.kubernetes.io
	// enforce/audit/warn labels. Left empty, the namespace gets no
	// PodSecurity labels at all, matching the tenant's isolation boundary
	// before this field existed.
	PodSecurityLevel PodSecurityLevel `json:"podSecurityLevel,omitempty"`
	AdminSubject     AdminSubject     `json:"adminSubject"`
}

// DefaultQuota returns a conservative, non-zero quota suitable for a
// freshly onboarded tenant.
func DefaultQuota() Quota {
	return Quota{
		CPURequest:    "2",
		MemoryRequest: "4Gi",
		CPULimit:      "4",
		MemoryLimit:   "8Gi",
		MaxPods:       "20",
	}
}

// DefaultLimitRange returns conservative per-container defaults.
func DefaultLimitRange() LimitRange {
	return LimitRange{
		DefaultCPURequest:    "100m",
		DefaultMemoryRequest: "128Mi",
		DefaultCPULimit:      "500m",
		DefaultMemoryLimit:   "512Mi",
	}
}
