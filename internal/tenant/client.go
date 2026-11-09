package tenant

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

// managedBySelector selects every object (namespace or otherwise) that
// tenantctl created.
const managedBySelector = TenantManagedByKey + "=" + ManagedByValue

// Client applies and removes tenant Bundles on a real or fake cluster. It
// depends only on kubernetes.Interface so it can be exercised in tests
// against k8s.io/client-go/kubernetes/fake without a live cluster.
type Client struct {
	kube kubernetes.Interface
}

// NewClient wraps an existing Kubernetes clientset.
func NewClient(kube kubernetes.Interface) *Client {
	return &Client{kube: kube}
}

// Info describes a tenant namespace as currently observed on the cluster.
type Info struct {
	Name      string
	Phase     string
	CreatedAt time.Time
	// Usage is the tenant's live ResourceQuota usage, one entry per
	// quota-managed resource. It is nil if the tenant's ResourceQuota
	// object could not be found (e.g. the namespace is still being
	// created or torn down).
	Usage []ResourceUsage
}

// ResourceUsage reports a tenant's observed consumption of one
// quota-managed resource against the hard limit from its ResourceQuota.
type ResourceUsage struct {
	Resource corev1.ResourceName
	Used     resource.Quantity
	Hard     resource.Quantity
}

// Percent returns Used as an integer percentage of Hard, rounded down. It
// returns 0 when Hard is zero rather than dividing by zero.
func (u ResourceUsage) Percent() int {
	hardMilli := u.Hard.MilliValue()
	if hardMilli == 0 {
		return 0
	}
	return int(u.Used.MilliValue() * 100 / hardMilli)
}

// quotaResourceOrder fixes the display order of a tenant's quota resources,
// matching the order buildResourceQuota sets them in.
var quotaResourceOrder = []corev1.ResourceName{
	corev1.ResourceRequestsCPU,
	corev1.ResourceRequestsMemory,
	corev1.ResourceLimitsCPU,
	corev1.ResourceLimitsMemory,
	corev1.ResourcePods,
}

// usageFromQuota extracts a ResourceUsage per hard-limited resource from a
// live ResourceQuota's Status, in quotaResourceOrder.
func usageFromQuota(rq *corev1.ResourceQuota) []ResourceUsage {
	usage := make([]ResourceUsage, 0, len(quotaResourceOrder))
	for _, name := range quotaResourceOrder {
		hard, ok := rq.Status.Hard[name]
		if !ok {
			continue
		}
		usage = append(usage, ResourceUsage{
			Resource: name,
			Used:     rq.Status.Used[name],
			Hard:     hard,
		})
	}
	return usage
}

// tenantUsage fetches ns's ResourceQuota and returns its usage, or nil
// (without error) if the ResourceQuota does not exist yet.
func (c *Client) tenantUsage(ctx context.Context, ns string) ([]ResourceUsage, error) {
	rq, err := c.kube.CoreV1().ResourceQuotas(ns).Get(ctx, ResourceQuotaName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get resource quota: %w", err)
	}
	return usageFromQuota(rq), nil
}

// CreateTenant applies every object in the Bundle. It fails if the
// tenant's namespace already exists, so re-running create against an
// existing tenant is a clear error rather than a silent partial update.
func (c *Client) CreateTenant(ctx context.Context, b Bundle) error {
	ns := b.Namespace.Name

	if _, err := c.kube.CoreV1().Namespaces().Create(ctx, b.Namespace, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("tenant %q already exists", ns)
		}
		return fmt.Errorf("create namespace: %w", err)
	}

	if _, err := c.kube.CoreV1().ResourceQuotas(ns).Create(ctx, b.ResourceQuota, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create resource quota: %w", err)
	}
	if _, err := c.kube.CoreV1().LimitRanges(ns).Create(ctx, b.LimitRange, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create limit range: %w", err)
	}
	for _, np := range b.NetworkPolicies {
		if _, err := c.kube.NetworkingV1().NetworkPolicies(ns).Create(ctx, np, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create network policy %q: %w", np.Name, err)
		}
	}
	if _, err := c.kube.RbacV1().Roles(ns).Create(ctx, b.Role, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	if _, err := c.kube.RbacV1().RoleBindings(ns).Create(ctx, b.RoleBinding, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create role binding: %w", err)
	}

	return nil
}

// UpdateTenant reconciles an existing tenant's quota, limit range, network
// policies, and RBAC to match the given Bundle. It never touches the
// Namespace object itself (a tenant's name is immutable), and it refuses to
// act on a namespace that is not labeled as tenantctl-managed, for the same
// reason DeleteTenant does.
func (c *Client) UpdateTenant(ctx context.Context, b Bundle) error {
	ns := b.Namespace.Name

	nsObj, err := c.kube.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tenant %q not found", ns)
		}
		return fmt.Errorf("get namespace: %w", err)
	}
	if nsObj.Labels[TenantManagedByKey] != ManagedByValue {
		return fmt.Errorf("namespace %q is not managed by tenantctl, refusing to update", ns)
	}

	currentRQ, err := c.kube.CoreV1().ResourceQuotas(ns).Get(ctx, b.ResourceQuota.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get resource quota: %w", err)
	}
	currentRQ.Spec = b.ResourceQuota.Spec
	currentRQ.Labels = b.ResourceQuota.Labels
	if _, err := c.kube.CoreV1().ResourceQuotas(ns).Update(ctx, currentRQ, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update resource quota: %w", err)
	}

	currentLR, err := c.kube.CoreV1().LimitRanges(ns).Get(ctx, b.LimitRange.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get limit range: %w", err)
	}
	currentLR.Spec = b.LimitRange.Spec
	currentLR.Labels = b.LimitRange.Labels
	if _, err := c.kube.CoreV1().LimitRanges(ns).Update(ctx, currentLR, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update limit range: %w", err)
	}

	if err := c.reconcileNetworkPolicies(ctx, ns, b.NetworkPolicies); err != nil {
		return err
	}

	currentRole, err := c.kube.RbacV1().Roles(ns).Get(ctx, b.Role.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get role: %w", err)
	}
	currentRole.Rules = b.Role.Rules
	currentRole.Labels = b.Role.Labels
	if _, err := c.kube.RbacV1().Roles(ns).Update(ctx, currentRole, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update role: %w", err)
	}

	currentRB, err := c.kube.RbacV1().RoleBindings(ns).Get(ctx, b.RoleBinding.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get role binding: %w", err)
	}
	currentRB.Subjects = b.RoleBinding.Subjects
	currentRB.Labels = b.RoleBinding.Labels
	if _, err := c.kube.RbacV1().RoleBindings(ns).Update(ctx, currentRB, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update role binding: %w", err)
	}

	return nil
}

// reconcileNetworkPolicies makes the tenantctl-managed NetworkPolicies in ns
// match desired exactly: policies no longer in desired (e.g. a removed
// cross-namespace allow-list) are deleted, policies already present are
// updated in place, and missing ones are created.
func (c *Client) reconcileNetworkPolicies(ctx context.Context, ns string, desired []*networkingv1.NetworkPolicy) error {
	existing, err := c.kube.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{
		LabelSelector: managedBySelector,
	})
	if err != nil {
		return fmt.Errorf("list network policies: %w", err)
	}

	desiredNames := make(map[string]bool, len(desired))
	for _, np := range desired {
		desiredNames[np.Name] = true
	}
	for _, np := range existing.Items {
		if desiredNames[np.Name] {
			continue
		}
		if err := c.kube.NetworkingV1().NetworkPolicies(ns).Delete(ctx, np.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete stale network policy %q: %w", np.Name, err)
		}
	}

	for _, np := range desired {
		current, err := c.kube.NetworkingV1().NetworkPolicies(ns).Get(ctx, np.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if _, err := c.kube.NetworkingV1().NetworkPolicies(ns).Create(ctx, np, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("create network policy %q: %w", np.Name, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("get network policy %q: %w", np.Name, err)
		}
		current.Spec = np.Spec
		current.Labels = np.Labels
		if _, err := c.kube.NetworkingV1().NetworkPolicies(ns).Update(ctx, current, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update network policy %q: %w", np.Name, err)
		}
	}
	return nil
}

// DiffTenant returns a line-level diff (see diffLines) between a tenant's
// live cluster state and the desired Bundle, without applying any changes.
// It refuses to act on a namespace that is not tenantctl-managed.
func (c *Client) DiffTenant(ctx context.Context, b Bundle) (string, error) {
	ns := b.Namespace.Name

	nsObj, err := c.kube.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("tenant %q not found", ns)
		}
		return "", fmt.Errorf("get namespace: %w", err)
	}
	if nsObj.Labels[TenantManagedByKey] != ManagedByValue {
		return "", fmt.Errorf("namespace %q is not managed by tenantctl, refusing to diff", ns)
	}

	var current, desired bytes.Buffer

	rq, rqErr := c.kube.CoreV1().ResourceQuotas(ns).Get(ctx, b.ResourceQuota.Name, metav1.GetOptions{})
	if rqErr == nil {
		stripServerFields(rq)
		rq.Status = corev1.ResourceQuotaStatus{}
	}
	if err := diffSection(&current, "ResourceQuota", b.ResourceQuota.Name, rq, rqErr); err != nil {
		return "", err
	}
	if err := diffSection(&desired, "ResourceQuota", b.ResourceQuota.Name, withoutTypeMeta(b.ResourceQuota), nil); err != nil {
		return "", err
	}

	lr, lrErr := c.kube.CoreV1().LimitRanges(ns).Get(ctx, b.LimitRange.Name, metav1.GetOptions{})
	if lrErr == nil {
		stripServerFields(lr)
	}
	if err := diffSection(&current, "LimitRange", b.LimitRange.Name, lr, lrErr); err != nil {
		return "", err
	}
	if err := diffSection(&desired, "LimitRange", b.LimitRange.Name, withoutTypeMeta(b.LimitRange), nil); err != nil {
		return "", err
	}

	for _, np := range b.NetworkPolicies {
		live, liveErr := c.kube.NetworkingV1().NetworkPolicies(ns).Get(ctx, np.Name, metav1.GetOptions{})
		if liveErr == nil {
			stripServerFields(live)
		}
		if err := diffSection(&current, "NetworkPolicy", np.Name, live, liveErr); err != nil {
			return "", err
		}
		if err := diffSection(&desired, "NetworkPolicy", np.Name, withoutTypeMeta(np), nil); err != nil {
			return "", err
		}
	}

	role, roleErr := c.kube.RbacV1().Roles(ns).Get(ctx, b.Role.Name, metav1.GetOptions{})
	if roleErr == nil {
		stripServerFields(role)
	}
	if err := diffSection(&current, "Role", b.Role.Name, role, roleErr); err != nil {
		return "", err
	}
	if err := diffSection(&desired, "Role", b.Role.Name, withoutTypeMeta(b.Role), nil); err != nil {
		return "", err
	}

	rb, rbErr := c.kube.RbacV1().RoleBindings(ns).Get(ctx, b.RoleBinding.Name, metav1.GetOptions{})
	if rbErr == nil {
		stripServerFields(rb)
	}
	if err := diffSection(&current, "RoleBinding", b.RoleBinding.Name, rb, rbErr); err != nil {
		return "", err
	}
	if err := diffSection(&desired, "RoleBinding", b.RoleBinding.Name, withoutTypeMeta(b.RoleBinding), nil); err != nil {
		return "", err
	}

	return diffLines(current.Bytes(), desired.Bytes()), nil
}

// stripServerFields clears the bookkeeping fields the API server populates
// on every object (resourceVersion, uid, creationTimestamp, generation,
// managedFields, selfLink) that are never present on a freshly built,
// unapplied Bundle object. Without this, DiffTenant's diff would be almost
// entirely server-side noise instead of the fields that actually drifted.
func stripServerFields(obj metav1.Object) {
	obj.SetResourceVersion("")
	obj.SetUID("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetGeneration(0)
	obj.SetManagedFields(nil)
	obj.SetSelfLink("")
}

// withoutTypeMeta returns a shallow copy of a Bundle object with its
// embedded TypeMeta cleared. Build() sets apiVersion/kind on every object so
// RenderYAML (used by `generate`) produces documents kubectl can apply
// standalone, but a typed client-go Get never populates TypeMeta on the
// object it returns. Without this, DiffTenant would report a spurious
// apiVersion/kind diff on every single object, every time, drowning out any
// real drift.
func withoutTypeMeta(obj interface{}) interface{} {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	cp := reflect.New(v.Type())
	cp.Elem().Set(v)
	if tm := cp.Elem().FieldByName("TypeMeta"); tm.IsValid() {
		tm.Set(reflect.Zero(tm.Type()))
	}
	return cp.Interface()
}

// diffSection marshals obj as a YAML document into buf, or, if err is an
// IsNotFound error, writes a "not found" marker instead. Any other error is
// returned so the caller can abort the diff.
func diffSection(buf *bytes.Buffer, kind, name string, obj interface{}, err error) error {
	if err != nil {
		if apierrors.IsNotFound(err) {
			fmt.Fprintf(buf, "# %s %q: not found\n", kind, name)
			return nil
		}
		return fmt.Errorf("get %s %q: %w", kind, name, err)
	}
	doc, merr := sigsyaml.Marshal(obj)
	if merr != nil {
		return fmt.Errorf("marshal %s %q: %w", kind, name, merr)
	}
	buf.Write(doc)
	buf.WriteString("---\n")
	return nil
}

// DeleteTenant deletes a tenant's namespace, which cascades deletion of
// every object inside it. It refuses to delete a namespace that is not
// labeled as tenantctl-managed, to avoid accidentally deleting an
// unrelated namespace that happens to share the tenant's name.
func (c *Client) DeleteTenant(ctx context.Context, name string) error {
	nsObj, err := c.kube.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("tenant %q not found", name)
		}
		return fmt.Errorf("get namespace: %w", err)
	}
	if nsObj.Labels[TenantManagedByKey] != ManagedByValue {
		return fmt.Errorf("namespace %q is not managed by tenantctl, refusing to delete", name)
	}

	if err := c.kube.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}
	return nil
}

// ListTenants returns every tenantctl-managed namespace, including each
// tenant's live ResourceQuota usage.
func (c *Client) ListTenants(ctx context.Context) ([]Info, error) {
	list, err := c.kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: managedBySelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	infos := make([]Info, 0, len(list.Items))
	for _, ns := range list.Items {
		usage, err := c.tenantUsage(ctx, ns.Name)
		if err != nil {
			return nil, fmt.Errorf("tenant %q: %w", ns.Name, err)
		}
		infos = append(infos, Info{
			Name:      ns.Labels[TenantLabelKey],
			Phase:     string(ns.Status.Phase),
			CreatedAt: ns.CreationTimestamp.Time,
			Usage:     usage,
		})
	}
	return infos, nil
}

// GetTenant returns Info, including live ResourceQuota usage, for a single
// tenantctl-managed tenant. It fails if the tenant does not exist or is not
// tenantctl-managed, for the same reason DeleteTenant does.
func (c *Client) GetTenant(ctx context.Context, name string) (Info, error) {
	nsObj, err := c.kube.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return Info{}, fmt.Errorf("tenant %q not found", name)
		}
		return Info{}, fmt.Errorf("get namespace: %w", err)
	}
	if nsObj.Labels[TenantManagedByKey] != ManagedByValue {
		return Info{}, fmt.Errorf("namespace %q is not managed by tenantctl", name)
	}

	usage, err := c.tenantUsage(ctx, name)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Name:      nsObj.Labels[TenantLabelKey],
		Phase:     string(nsObj.Status.Phase),
		CreatedAt: nsObj.CreationTimestamp.Time,
		Usage:     usage,
	}, nil
}
