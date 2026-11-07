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
	CPURequest    string
	MemoryRequest string
	CPULimit      string
	MemoryLimit   string
	// MaxPods bounds the number of pods the namespace may run concurrently.
	MaxPods string
}

// LimitRange sets the per-container defaults and bounds applied when a pod
// in the tenant's namespace omits its own resource requests/limits.
type LimitRange struct {
	DefaultCPURequest    string
	DefaultMemoryRequest string
	DefaultCPULimit      string
	DefaultMemoryLimit   string
}

// AdminSubject is the RBAC subject bound to the tenant-admin Role in the
// tenant's namespace.
type AdminSubject struct {
	Kind      SubjectKind
	Name      string
	Namespace string // required when Kind == SubjectKindServiceAccount
}

// Spec is the declarative, cluster-independent description of a tenant.
// Everything tenantctl builds and applies is derived from a Spec.
type Spec struct {
	Name         string
	Quota        Quota
	LimitRange   LimitRange
	AdminSubject AdminSubject
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
