package tenant

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

// ListTenants returns every tenantctl-managed namespace.
func (c *Client) ListTenants(ctx context.Context) ([]Info, error) {
	list, err := c.kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: managedBySelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	infos := make([]Info, 0, len(list.Items))
	for _, ns := range list.Items {
		infos = append(infos, Info{
			Name:      ns.Labels[TenantLabelKey],
			Phase:     string(ns.Status.Phase),
			CreatedAt: ns.CreationTimestamp.Time,
		})
	}
	return infos, nil
}
