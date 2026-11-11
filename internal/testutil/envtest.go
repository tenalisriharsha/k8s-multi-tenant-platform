// Package testutil provides a reusable envtest bootstrap for integration
// tests that need a real (but ephemeral) Kubernetes API server rather than
// the fake clientset used by internal/tenant's unit tests.
//
// tenantctl only manages built-in API types (Namespace, ResourceQuota,
// LimitRange, NetworkPolicy, Role, RoleBinding) and installs no CRDs, so
// the envtest.Environment this package returns needs no CRDDirectoryPaths
// or scheme registration beyond client-go's defaults. It requires
// kubebuilder's etcd/kube-apiserver test binaries on disk, referenced via
// the KUBEBUILDER_ASSETS environment variable (see
// sigs.k8s.io/controller-runtime/tools/setup-envtest, wired into this
// project's CI in .github/workflows/ci.yml); it does not require Docker or
// a running kind cluster, which is what makes it suited to CI unlike
// tests/e2e_kind_test.go's "e2e" build tag.
package testutil

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// NewEnvtestEnvironment returns an unstarted envtest.Environment configured
// for this project. Call Start on it to bring up a real API server and
// obtain a *rest.Config, and Stop to tear it down again.
func NewEnvtestEnvironment() *envtest.Environment {
	return &envtest.Environment{}
}

// NewClientset builds a typed kubernetes.Interface from the *rest.Config
// returned by envtest.Environment.Start, for use with internal/tenant's
// Client (which takes a kubernetes.Interface so production code and tests
// can share the same code path against either a real API server or a
// client-go fake).
func NewClientset(cfg *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(cfg)
}
