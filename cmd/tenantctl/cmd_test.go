package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/tenalisriharsha/k8s-multi-tenant-platform/internal/tenant"
)

func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (stdout string, err error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return buf.String(), err
}

func TestGenerate_RequiresNameAndAdminName(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing both", nil},
		{"missing admin-name", []string{"--name", "acme"}},
		{"missing name", []string{"--admin-name", "acme-admins"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCmd(t, newGenerateCmd(), c.args...)
			if err == nil {
				t.Fatalf("expected error for args %v, got nil", c.args)
			}
		})
	}
}

func TestGenerate_WritesYAMLToStdout(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(), "--name", "acme-corp", "--admin-name", "acme-admins")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if !strings.Contains(out, "kind: Namespace") {
		t.Errorf("stdout missing rendered Namespace, got:\n%s", out)
	}
	if !strings.Contains(out, "acme-corp") {
		t.Errorf("stdout does not mention tenant name, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: RoleBinding") {
		t.Errorf("stdout missing rendered RoleBinding, got:\n%s", out)
	}
}

func TestGenerate_InvalidNameFailsBeforeRendering(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(), "--name", "Not_Valid", "--admin-name", "acme-admins")
	if err == nil {
		t.Fatal("expected validation error for an invalid tenant name")
	}
	if strings.Contains(out, "kind:") {
		t.Errorf("expected no YAML to be rendered for an invalid spec, got:\n%s", out)
	}
}

func TestGenerate_OutputFlagWritesFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "bundle.yaml")

	stdout, err := runCmd(t, newGenerateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins", "--output", outPath)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if stdout != "" {
		t.Errorf("expected nothing on stdout when --output is set, got:\n%s", stdout)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected --output to write a file: %v", err)
	}
	if !strings.Contains(string(data), "kind: Namespace") {
		t.Errorf("output file missing rendered Namespace, got:\n%s", data)
	}
}

func TestGenerate_DefaultsMatchDocumentedValues(t *testing.T) {
	flags := &specFlags{}
	cmd := &cobra.Command{Use: "generate"}
	addSpecFlags(cmd, flags)

	flags.name = "acme-corp"
	flags.adminName = "acme-admins"
	spec := flags.toSpec()

	want := map[string]string{
		"cpu-request":            "2",
		"memory-request":         "4Gi",
		"cpu-limit":              "4",
		"memory-limit":           "8Gi",
		"max-pods":               "20",
		"default-cpu-request":    "100m",
		"default-memory-request": "128Mi",
		"default-cpu-limit":      "500m",
		"default-memory-limit":   "512Mi",
	}
	got := map[string]string{
		"cpu-request":            spec.Quota.CPURequest,
		"memory-request":         spec.Quota.MemoryRequest,
		"cpu-limit":              spec.Quota.CPULimit,
		"memory-limit":           spec.Quota.MemoryLimit,
		"max-pods":               spec.Quota.MaxPods,
		"default-cpu-request":    spec.LimitRange.DefaultCPURequest,
		"default-memory-request": spec.LimitRange.DefaultMemoryRequest,
		"default-cpu-limit":      spec.LimitRange.DefaultCPULimit,
		"default-memory-limit":   spec.LimitRange.DefaultMemoryLimit,
	}
	for flag, wantVal := range want {
		if got[flag] != wantVal {
			t.Errorf("default for --%s = %q, want %q (per README flag table)", flag, got[flag], wantVal)
		}
	}
	if flags.adminKind != "Group" {
		t.Errorf("default --admin-kind = %q, want %q", flags.adminKind, "Group")
	}
	if flags.podSecurity != "restricted" {
		t.Errorf("default --pod-security = %q, want %q", flags.podSecurity, "restricted")
	}
	if spec.PodSecurityLevel != tenant.PodSecurityRestricted {
		t.Errorf("default spec.PodSecurityLevel = %q, want %q", spec.PodSecurityLevel, tenant.PodSecurityRestricted)
	}
}

func TestSpecFlags_ToSpec_PodSecurityNoneMapsToEmptyLevel(t *testing.T) {
	flags := &specFlags{name: "acme-corp", adminName: "acme-admins", adminKind: "Group", podSecurity: "none"}
	spec := flags.toSpec()
	if spec.PodSecurityLevel != "" {
		t.Errorf("--pod-security none should map to an empty PodSecurityLevel (PSA labels disabled), got %q", spec.PodSecurityLevel)
	}
}

func TestSpecFlags_ToSpec_PodSecurityLevelsPassThrough(t *testing.T) {
	for _, level := range []string{"privileged", "baseline", "restricted"} {
		t.Run(level, func(t *testing.T) {
			flags := &specFlags{name: "acme-corp", adminName: "acme-admins", adminKind: "Group", podSecurity: level}
			spec := flags.toSpec()
			if string(spec.PodSecurityLevel) != level {
				t.Errorf("--pod-security %s should pass through unchanged, got PodSecurityLevel=%q", level, spec.PodSecurityLevel)
			}
		})
	}
}

func TestPodSecurityLabel_FormatsForDisplay(t *testing.T) {
	cases := []struct {
		level tenant.PodSecurityLevel
		want  string
	}{
		{"", "none"},
		{tenant.PodSecurityRestricted, "restricted"},
		{tenant.PodSecurityBaseline, "baseline"},
		{tenant.PodSecurityPrivileged, "privileged"},
	}
	for _, c := range cases {
		s := tenant.Spec{PodSecurityLevel: c.level}
		if got := podSecurityLabel(s); got != c.want {
			t.Errorf("podSecurityLabel(PodSecurityLevel=%q) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestGenerate_InvalidPodSecurityLevelFailsValidation(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins", "--pod-security", "super-locked-down")
	if err == nil {
		t.Fatal("expected a validation error for an invalid --pod-security value")
	}
	if strings.Contains(out, "kind:") {
		t.Errorf("expected no YAML to be rendered for an invalid spec, got:\n%s", out)
	}
}

func TestGenerate_PodSecurityDefaultRestrictedAppliesAllThreeLabels(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(), "--name", "acme-corp", "--admin-name", "acme-admins")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	for _, line := range []string{
		"pod-security.kubernetes.io/enforce: restricted",
		"pod-security.kubernetes.io/audit: restricted",
		"pod-security.kubernetes.io/warn: restricted",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("expected rendered Namespace to contain %q by default, got:\n%s", line, out)
		}
	}
}

func TestGenerate_PodSecurityNoneOmitsLabels(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins", "--pod-security", "none")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if strings.Contains(out, "pod-security.kubernetes.io") {
		t.Errorf("expected --pod-security none to omit all PSA labels, got:\n%s", out)
	}
}

func TestRootCmd_HasAllSixSubcommands(t *testing.T) {
	// README.md advertises "the six-command CLI surface"; a command missing
	// here must fail this test rather than silently shrinking the surface.
	root := newRootCmd()
	want := map[string]bool{
		"generate": false, "create": false, "update": false,
		"delete": false, "list": false, "status": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("root command missing subcommand %q", name)
		}
	}
	if got := len(root.Commands()); got != len(want) {
		t.Errorf("root command has %d subcommands, want exactly %d", got, len(want))
	}
}

func TestGenerate_AllowNamespaceFlagAddsCrossNamespacePolicy(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--allow-namespace", "ingress-nginx", "--allow-namespace", "shared-tools")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if !strings.Contains(out, "allow-cross-namespace") {
		t.Errorf("expected --allow-namespace to produce an allow-cross-namespace policy, got:\n%s", out)
	}
	if !strings.Contains(out, "ingress-nginx") || !strings.Contains(out, "shared-tools") {
		t.Errorf("expected both allow-listed namespaces to appear in the rendered bundle, got:\n%s", out)
	}
}

func TestGenerate_NoAllowNamespaceOmitsCrossNamespacePolicy(t *testing.T) {
	out, err := runCmd(t, newGenerateCmd(), "--name", "acme-corp", "--admin-name", "acme-admins")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if strings.Contains(out, "allow-cross-namespace") {
		t.Errorf("expected no allow-cross-namespace policy without --allow-namespace, got:\n%s", out)
	}
}

func TestGenerate_FileFlagLoadsSpecFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	const data = `
name: file-corp
quota: {cpuRequest: "2", memoryRequest: 4Gi, cpuLimit: "4", memoryLimit: 8Gi, maxPods: "20"}
limitRange: {defaultCPURequest: 100m, defaultMemoryRequest: 128Mi, defaultCPULimit: 500m, defaultMemoryLimit: 512Mi}
adminSubject: {kind: Group, name: file-admins}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	// --file alone, with no --name/--admin-name at all, must be enough.
	out, err := runCmd(t, newGenerateCmd(), "--file", path)
	if err != nil {
		t.Fatalf("generate --file failed: %v", err)
	}
	if !strings.Contains(out, "file-corp") {
		t.Errorf("expected rendered bundle to mention the tenant name from the file, got:\n%s", out)
	}
}

func TestGenerate_ExampleFileFromREADMEWorks(t *testing.T) {
	// examples/tenant.yaml is the exact file README.md and PROGRESS.md point
	// readers at for --file/-f; confirm it actually works through the CLI,
	// not just through tenant.LoadSpecFile directly.
	out, err := runCmd(t, newGenerateCmd(), "-f", filepath.Join("..", "..", "examples", "tenant.yaml"))
	if err != nil {
		t.Fatalf("generate -f examples/tenant.yaml failed: %v", err)
	}
	if !strings.Contains(out, "acme-corp") {
		t.Errorf("expected rendered bundle to mention acme-corp, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: NetworkPolicy") {
		t.Errorf("expected rendered bundle to include network policies, got:\n%s", out)
	}
}

func TestUpdate_RequiresNameAndAdminNameWithoutFile(t *testing.T) {
	_, err := runCmd(t, newUpdateCmd(), "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when --name/--admin-name are not set and --file is not used")
	}
}

func TestUpdate_FailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newUpdateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestUpdate_ValidatesSpecBeforeTouchingCluster(t *testing.T) {
	_, err := runCmd(t, newUpdateCmd(),
		"--name", "Not_Valid", "--admin-name", "acme-admins",
		"--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected a validation error for an invalid tenant name")
	}
	if strings.Contains(err.Error(), "kubeconfig") {
		t.Errorf("expected validation to fail before the kubeconfig is even loaded, got: %v", err)
	}
}

func TestUpdate_DryRunNeverContactsCluster(t *testing.T) {
	// --dry-run must render and exit before newClientset is ever called, so
	// an unreachable kubeconfig must not cause this to fail.
	out, err := runCmd(t, newUpdateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--dry-run", "--kubeconfig", unreachableKubeconfig(t))
	if err != nil {
		t.Fatalf("update --dry-run should succeed without contacting a cluster, got: %v", err)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected a dry-run marker in output, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: ResourceQuota") {
		t.Errorf("expected the rendered bundle in dry-run output, got:\n%s", out)
	}
}

func TestUpdate_DiffRequiresCluster(t *testing.T) {
	// Unlike --dry-run, --diff must fetch live state, so it should fail the
	// same way as a normal update when the cluster can't be reached.
	_, err := runCmd(t, newUpdateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--diff", "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected --diff to fail when the kubeconfig cannot be loaded, since it must contact the cluster")
	}
}

func TestCreate_DryRunNeverContactsCluster(t *testing.T) {
	out, err := runCmd(t, newCreateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--dry-run", "--kubeconfig", unreachableKubeconfig(t))
	if err != nil {
		t.Fatalf("create --dry-run should succeed without contacting a cluster, got: %v", err)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected a dry-run marker in output, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: Namespace") {
		t.Errorf("expected the rendered bundle in dry-run output, got:\n%s", out)
	}
}

// Cluster-targeting commands (create/delete/list) should fail with a clear
// error rather than hang or panic when the kubeconfig cannot be loaded, so
// none of these tests need a real or fake cluster to exercise that path.
func unreachableKubeconfig(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "does-not-exist.kubeconfig")
}

func TestCreate_FailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newCreateCmd(),
		"--name", "acme-corp", "--admin-name", "acme-admins",
		"--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestCreate_ValidatesSpecBeforeTouchingCluster(t *testing.T) {
	_, err := runCmd(t, newCreateCmd(),
		"--name", "Not_Valid", "--admin-name", "acme-admins",
		"--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected a validation error for an invalid tenant name")
	}
	if strings.Contains(err.Error(), "kubeconfig") {
		t.Errorf("expected validation to fail before the kubeconfig is even loaded, got: %v", err)
	}
}

func TestDelete_RequiresName(t *testing.T) {
	_, err := runCmd(t, newDeleteCmd(), "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when --name is not set")
	}
}

func TestDelete_FailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newDeleteCmd(), "--name", "acme-corp", "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestList_FailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newListCmd(), "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestStatus_FailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newStatusCmd(), "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestStatus_WithNameFailsFastOnBadKubeconfig(t *testing.T) {
	_, err := runCmd(t, newStatusCmd(), "acme-corp", "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected an error when the kubeconfig path does not exist")
	}
}

func TestStatus_RejectsMoreThanOneArg(t *testing.T) {
	_, err := runCmd(t, newStatusCmd(), "acme-corp", "extra-arg", "--kubeconfig", unreachableKubeconfig(t))
	if err == nil {
		t.Fatal("expected status to reject more than one positional argument (README: `status [name]`)")
	}
}

func TestPeakUtilization_NoUsageReturnsDash(t *testing.T) {
	if got := peakUtilization(nil); got != "-" {
		t.Errorf("peakUtilization(nil) = %q, want %q", got, "-")
	}
	if got := peakUtilization([]tenant.ResourceUsage{}); got != "-" {
		t.Errorf("peakUtilization(empty) = %q, want %q", got, "-")
	}
}

func TestPeakUtilization_PicksMostUtilizedResource(t *testing.T) {
	usage := []tenant.ResourceUsage{
		{Resource: corev1.ResourceRequestsCPU, Used: resource.MustParse("1"), Hard: resource.MustParse("4")},        // 25%
		{Resource: corev1.ResourceRequestsMemory, Used: resource.MustParse("3Gi"), Hard: resource.MustParse("4Gi")}, // 75%
		{Resource: corev1.ResourcePods, Used: resource.MustParse("2"), Hard: resource.MustParse("20")},              // 10%
	}
	got := peakUtilization(usage)
	want := "memory 75%"
	if got != want {
		t.Errorf("peakUtilization(...) = %q, want %q", got, want)
	}
}

func TestPeakUtilization_UndottedResourceNameIsUnchanged(t *testing.T) {
	// "pods" has no "requests."/"limits." prefix to strip, unlike the
	// quota keys in TestPeakUtilization_PicksMostUtilizedResource; this
	// exercises shortResourceName's no-dot fallback path.
	usage := []tenant.ResourceUsage{
		{Resource: corev1.ResourceRequestsCPU, Used: resource.MustParse("1"), Hard: resource.MustParse("4")}, // 25%
		{Resource: corev1.ResourcePods, Used: resource.MustParse("18"), Hard: resource.MustParse("20")},      // 90%
	}
	got := peakUtilization(usage)
	want := "pods 90%"
	if got != want {
		t.Errorf("peakUtilization(...) = %q, want %q", got, want)
	}
}

func TestClusterFlags_NewClientset_ValidKubeconfig(t *testing.T) {
	// A well-formed kubeconfig should build a clientset without dialing
	// the server, exercising the real client-go loading path end to end.
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	const kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	f := &clusterFlags{kubeconfig: kubeconfigPath}
	clientset, err := f.newClientset()
	if err != nil {
		t.Fatalf("newClientset with a valid kubeconfig should succeed without contacting a server: %v", err)
	}
	if clientset == nil {
		t.Fatal("expected a non-nil clientset")
	}
}
