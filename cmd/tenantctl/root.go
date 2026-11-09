package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

// specFlags holds the CLI flags shared by generate, create, and update,
// since all three build a tenant.Spec from user input before doing
// something with it.
type specFlags struct {
	file string
	name string

	cpuRequest    string
	memoryRequest string
	cpuLimit      string
	memoryLimit   string
	maxPods       string

	defaultCPURequest    string
	defaultMemoryRequest string
	defaultCPULimit      string
	defaultMemoryLimit   string

	allowedNamespaces []string

	adminKind      string
	adminName      string
	adminNamespace string
}

func addSpecFlags(cmd *cobra.Command, f *specFlags) {
	dq := tenant.DefaultQuota()
	dl := tenant.DefaultLimitRange()

	cmd.Flags().StringVarP(&f.file, "file", "f", "", "load the tenant spec from a YAML file instead of flags (GitOps mode); when set, --name and --admin-name are not required and the other spec flags below are ignored")

	cmd.Flags().StringVar(&f.name, "name", "", "tenant name (required unless --file is set; must be a valid DNS label)")

	cmd.Flags().StringVar(&f.cpuRequest, "cpu-request", dq.CPURequest, "total CPU requests allowed in the tenant namespace")
	cmd.Flags().StringVar(&f.memoryRequest, "memory-request", dq.MemoryRequest, "total memory requests allowed in the tenant namespace")
	cmd.Flags().StringVar(&f.cpuLimit, "cpu-limit", dq.CPULimit, "total CPU limits allowed in the tenant namespace")
	cmd.Flags().StringVar(&f.memoryLimit, "memory-limit", dq.MemoryLimit, "total memory limits allowed in the tenant namespace")
	cmd.Flags().StringVar(&f.maxPods, "max-pods", dq.MaxPods, "maximum number of pods allowed in the tenant namespace")

	cmd.Flags().StringVar(&f.defaultCPURequest, "default-cpu-request", dl.DefaultCPURequest, "default per-container CPU request")
	cmd.Flags().StringVar(&f.defaultMemoryRequest, "default-memory-request", dl.DefaultMemoryRequest, "default per-container memory request")
	cmd.Flags().StringVar(&f.defaultCPULimit, "default-cpu-limit", dl.DefaultCPULimit, "default per-container CPU limit")
	cmd.Flags().StringVar(&f.defaultMemoryLimit, "default-memory-limit", dl.DefaultMemoryLimit, "default per-container memory limit")

	cmd.Flags().StringArrayVar(&f.allowedNamespaces, "allow-namespace", nil, "additional namespace allowed bidirectional NetworkPolicy traffic with this tenant (repeatable), e.g. a shared ingress controller namespace")

	cmd.Flags().StringVar(&f.adminKind, "admin-kind", "Group", "RBAC subject kind bound as tenant admin: User, Group, or ServiceAccount")
	cmd.Flags().StringVar(&f.adminName, "admin-name", "", "RBAC subject name bound as tenant admin (required unless --file is set)")
	cmd.Flags().StringVar(&f.adminNamespace, "admin-namespace", "", "namespace of the admin ServiceAccount (required when --admin-kind=ServiceAccount)")
}

func (f *specFlags) toSpec() tenant.Spec {
	return tenant.Spec{
		Name: f.name,
		Quota: tenant.Quota{
			CPURequest:    f.cpuRequest,
			MemoryRequest: f.memoryRequest,
			CPULimit:      f.cpuLimit,
			MemoryLimit:   f.memoryLimit,
			MaxPods:       f.maxPods,
		},
		LimitRange: tenant.LimitRange{
			DefaultCPURequest:    f.defaultCPURequest,
			DefaultMemoryRequest: f.defaultMemoryRequest,
			DefaultCPULimit:      f.defaultCPULimit,
			DefaultMemoryLimit:   f.defaultMemoryLimit,
		},
		AllowedNamespaces: f.allowedNamespaces,
		AdminSubject: tenant.AdminSubject{
			Kind:      tenant.SubjectKind(f.adminKind),
			Name:      f.adminName,
			Namespace: f.adminNamespace,
		},
	}
}

// resolveSpec builds the tenant.Spec that generate/create/update will act
// on: from --file when set, otherwise from the rest of the spec flags. It
// deliberately does not use cobra's MarkFlagRequired for --name/--admin-name
// so that --file can satisfy them instead.
func (f *specFlags) resolveSpec() (tenant.Spec, error) {
	if f.file != "" {
		return tenant.LoadSpecFile(f.file)
	}
	if f.name == "" {
		return tenant.Spec{}, fmt.Errorf("required flag \"name\" not set (or use --file to load a spec from a file)")
	}
	if f.adminName == "" {
		return tenant.Spec{}, fmt.Errorf("required flag \"admin-name\" not set (or use --file to load a spec from a file)")
	}
	return f.toSpec(), nil
}

// clusterFlags holds the CLI flags shared by every subcommand that talks
// to a real cluster (create, delete, list).
type clusterFlags struct {
	kubeconfig string
	context    string
}

func addClusterFlags(cmd *cobra.Command, f *clusterFlags) {
	cmd.Flags().StringVar(&f.kubeconfig, "kubeconfig", "", "path to kubeconfig (defaults to $KUBECONFIG or ~/.kube/config)")
	cmd.Flags().StringVar(&f.context, "context", "", "kubeconfig context to use (defaults to the current context)")
}

func (f *clusterFlags) newClientset() (kubernetes.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.kubeconfig != "" {
		loadingRules.ExplicitPath = f.kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if f.context != "" {
		overrides.CurrentContext = f.context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}
	return clientset, nil
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tenantctl",
		Short:         "Automate onboarding and removing namespace-isolated Kubernetes tenants",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newGenerateCmd())
	root.AddCommand(newCreateCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newStatusCmd())

	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
