//go:build e2e

// Package e2e exercises the documented tenantctl workflows against a real
// Kubernetes API server (a local kind cluster), with no client-go fake and
// no mocked internal code: it builds the real tenantctl binary and drives
// it exactly as a user would, per README.md's "Quick start" and "What gets
// created" sections.
//
// It is gated behind the "e2e" build tag (not part of `go test ./...`,
// mirroring the project's own CI, which has no docker/kind available) and
// requires `kind` and `kubectl` on PATH plus a running docker daemon. Run
// it explicitly with:
//
//	go test -tags e2e ./tests/... -v -timeout 5m
package e2e

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const clusterName = "tenantctl-e2e-test"

func run(t *testing.T, timeout time.Duration, dir, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func mustRun(t *testing.T, timeout time.Duration, dir, name string, args ...string) string {
	t.Helper()
	out, err := run(t, timeout, dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found on PATH, skipping real-cluster end-to-end test", name)
	}
}

// TestTenantctlAgainstRealCluster reproduces the full tenant lifecycle the
// README's Quick start walks through (create, list, delete), plus the
// safety guard against deleting a namespace tenantctl doesn't manage, all
// against a live kind cluster's real API server. No part of tenantctl's
// own code is mocked; only the cluster is real infrastructure spun up for
// the test.
func TestTenantctlAgainstRealCluster(t *testing.T) {
	requireBinary(t, "kind")
	requireBinary(t, "kubectl")
	requireBinary(t, "docker")

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	workDir := t.TempDir()
	kubeconfig := filepath.Join(workDir, "kubeconfig")
	binary := filepath.Join(workDir, "tenantctl")

	// Always tear the cluster down, even if setup or an assertion fails
	// partway through, so no stray containers are left behind.
	t.Cleanup(func() {
		out, err := run(t, 60*time.Second, repoRoot, "kind", "delete", "cluster", "--name", clusterName, "--kubeconfig", kubeconfig)
		if err != nil {
			t.Logf("cleanup: kind delete cluster failed: %v\n%s", err, out)
		}
	})

	mustRun(t, 15*time.Second, repoRoot, "go", "build", "-o", binary, "./cmd/tenantctl")

	mustRun(t, 180*time.Second, repoRoot, "kind", "create", "cluster",
		"--name", clusterName, "--kubeconfig", kubeconfig, "--wait", "90s")

	ctxArgs := []string{"--kubeconfig", kubeconfig, "--context", "kind-" + clusterName}

	t.Run("create applies the full bundle", func(t *testing.T) {
		out := mustRun(t, 30*time.Second, repoRoot, binary, append([]string{
			"create", "--name", "e2e-corp", "--admin-name", "e2e-admins",
		}, ctxArgs...)...)
		if !strings.Contains(out, `"e2e-corp"`) || !strings.Contains(out, "created") {
			t.Errorf("unexpected create output: %s", out)
		}

		kubectlGet := func(args ...string) string {
			return mustRun(t, 15*time.Second, repoRoot, "kubectl",
				append([]string{"--kubeconfig", kubeconfig}, args...)...)
		}

		nsLabels := kubectlGet("get", "namespace", "e2e-corp", "-o", "jsonpath={.metadata.labels}")
		if !strings.Contains(nsLabels, "tenant-platform.io/tenant") || !strings.Contains(nsLabels, "tenantctl") {
			t.Errorf("namespace missing expected labels: %s", nsLabels)
		}

		rqHard := kubectlGet("get", "resourcequota", "tenant-quota", "-n", "e2e-corp", "-o", "jsonpath={.spec.hard}")
		if !strings.Contains(rqHard, `"pods":"20"`) {
			t.Errorf("resource quota hard limits don't match the documented default max-pods=20: %s", rqHard)
		}

		for _, np := range []string{"deny-all", "allow-same-namespace", "allow-dns-egress"} {
			kubectlGet("get", "networkpolicy", np, "-n", "e2e-corp")
		}
		kubectlGet("get", "limitrange", "tenant-limits", "-n", "e2e-corp")
		kubectlGet("get", "role", "tenant-admin", "-n", "e2e-corp")
		kubectlGet("get", "rolebinding", "tenant-admin-binding", "-n", "e2e-corp")
	})

	t.Run("pod security admission: default restricted rejects a privileged pod, update --pod-security baseline allows a plain one", func(t *testing.T) {
		kubectlGet := func(args ...string) string {
			return mustRun(t, 15*time.Second, repoRoot, "kubectl",
				append([]string{"--kubeconfig", kubeconfig}, args...)...)
		}

		nsLabels := kubectlGet("get", "namespace", "e2e-corp", "-o", "jsonpath={.metadata.labels}")
		if !strings.Contains(nsLabels, `"pod-security.kubernetes.io/enforce":"restricted"`) {
			t.Errorf("expected the tenant namespace to default to the restricted Pod Security Admission level, got labels: %s", nsLabels)
		}

		privilegedOverrides := `{"spec":{"containers":[{"name":"psa-reject-test","image":"busybox:1.36","securityContext":{"privileged":true}}]}}`
		out, err := run(t, 30*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
			"run", "psa-reject-test", "-n", "e2e-corp", "--image=busybox:1.36", "--restart=Never",
			"--overrides="+privilegedOverrides)
		if err == nil {
			t.Fatalf("expected a privileged pod to be rejected by the restricted namespace's PodSecurity admission, but it was created:\n%s", out)
		}
		if !strings.Contains(out, `violates PodSecurity "restricted:latest"`) {
			t.Errorf("expected a PodSecurity admission rejection mentioning the restricted level, got:\n%s", out)
		}

		out = mustRun(t, 30*time.Second, repoRoot, binary, append([]string{
			"update", "--name", "e2e-corp", "--admin-name", "e2e-admins", "--pod-security", "baseline",
		}, ctxArgs...)...)
		if !strings.Contains(out, "pod-security=baseline") || !strings.Contains(out, "updated") {
			t.Errorf("unexpected update --pod-security baseline output: %s", out)
		}

		nsLabels = kubectlGet("get", "namespace", "e2e-corp", "-o", "jsonpath={.metadata.labels}")
		if !strings.Contains(nsLabels, `"pod-security.kubernetes.io/enforce":"baseline"`) {
			t.Errorf("expected update to actually relabel the namespace to baseline, got labels: %s", nsLabels)
		}

		// A plain pod with no special securityContext violates restricted
		// (missing runAsNonRoot/seccompProfile/dropped capabilities) but is
		// allowed under baseline, which only blocks known privilege
		// escalations (privileged, hostNetwork, hostPath, ...). This proves
		// the relabel was actually enforced by the admission controller, not
		// just reflected in the namespace object.
		mustRun(t, 30*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
			"run", "psa-allow-test", "-n", "e2e-corp", "--image=busybox:1.36", "--restart=Never",
			"--command", "--", "sleep", "60")
		t.Cleanup(func() {
			_, _ = run(t, 15*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
				"delete", "pod", "psa-allow-test", "-n", "e2e-corp", "--ignore-not-found", "--wait=false")
		})
	})

	t.Run("create refuses a namespace that already exists", func(t *testing.T) {
		out, err := run(t, 30*time.Second, repoRoot, binary, append([]string{
			"create", "--name", "e2e-corp", "--admin-name", "e2e-admins",
		}, ctxArgs...)...)
		if err == nil {
			t.Fatalf("expected create to fail for an already-onboarded tenant, got:\n%s", out)
		}
		if !strings.Contains(out, "already exists") {
			t.Errorf("expected an 'already exists' error, got:\n%s", out)
		}
	})

	t.Run("list shows the onboarded tenant", func(t *testing.T) {
		out := mustRun(t, 30*time.Second, repoRoot, binary, append([]string{"list"}, ctxArgs...)...)
		if !strings.Contains(out, "e2e-corp") {
			t.Errorf("expected list output to mention e2e-corp, got:\n%s", out)
		}
	})

	t.Run("delete refuses an unmanaged namespace", func(t *testing.T) {
		mustRun(t, 15*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
			"create", "namespace", "unmanaged-ns")
		defer mustRun(t, 15*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
			"delete", "namespace", "unmanaged-ns", "--wait=false")

		out, err := run(t, 30*time.Second, repoRoot, binary, append([]string{
			"delete", "--name", "unmanaged-ns",
		}, ctxArgs...)...)
		if err == nil {
			t.Fatalf("expected delete to refuse a namespace tenantctl did not create, got:\n%s", out)
		}
		if !strings.Contains(out, "not managed by tenantctl") {
			t.Errorf("expected a 'not managed by tenantctl' error, got:\n%s", out)
		}
	})

	t.Run("delete removes the tenant namespace", func(t *testing.T) {
		out := mustRun(t, 30*time.Second, repoRoot, binary, append([]string{
			"delete", "--name", "e2e-corp",
		}, ctxArgs...)...)
		if !strings.Contains(out, "deleted") {
			t.Errorf("unexpected delete output: %s", out)
		}

		// Namespace deletion in real Kubernetes is asynchronous (Terminating
		// phase), so poll briefly rather than asserting it is gone instantly.
		deadline := time.Now().Add(60 * time.Second)
		gone := false
		for time.Now().Before(deadline) {
			_, getErr := run(t, 10*time.Second, repoRoot, "kubectl", "--kubeconfig", kubeconfig,
				"get", "namespace", "e2e-corp")
			if getErr != nil {
				gone = true
				break
			}
			time.Sleep(3 * time.Second)
		}
		if !gone {
			t.Fatal("namespace e2e-corp was never observed to finish deleting")
		}
	})
}
