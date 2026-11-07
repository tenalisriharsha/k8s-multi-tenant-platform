package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
}

func TestRootCmd_HasAllFourSubcommands(t *testing.T) {
	root := newRootCmd()
	want := map[string]bool{"generate": false, "create": false, "delete": false, "list": false}
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
