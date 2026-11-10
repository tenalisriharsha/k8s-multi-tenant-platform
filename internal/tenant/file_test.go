package tenant

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSpecFile_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	const data = `
name: acme-corp
quota:
  cpuRequest: "2"
  memoryRequest: 4Gi
  cpuLimit: "4"
  memoryLimit: 8Gi
  maxPods: "20"
limitRange:
  defaultCPURequest: 100m
  defaultMemoryRequest: 128Mi
  defaultCPULimit: 500m
  defaultMemoryLimit: 512Mi
allowedNamespaces:
  - ingress-nginx
adminSubject:
  kind: Group
  name: acme-admins
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	spec, err := LoadSpecFile(path)
	if err != nil {
		t.Fatalf("LoadSpecFile: %v", err)
	}

	if spec.Name != "acme-corp" {
		t.Errorf("Name = %q, want %q", spec.Name, "acme-corp")
	}
	if spec.Quota.CPURequest != "2" || spec.Quota.MemoryRequest != "4Gi" ||
		spec.Quota.CPULimit != "4" || spec.Quota.MemoryLimit != "8Gi" || spec.Quota.MaxPods != "20" {
		t.Errorf("Quota = %+v, unexpected", spec.Quota)
	}
	if spec.LimitRange.DefaultCPURequest != "100m" || spec.LimitRange.DefaultMemoryRequest != "128Mi" ||
		spec.LimitRange.DefaultCPULimit != "500m" || spec.LimitRange.DefaultMemoryLimit != "512Mi" {
		t.Errorf("LimitRange = %+v, unexpected", spec.LimitRange)
	}
	if len(spec.AllowedNamespaces) != 1 || spec.AllowedNamespaces[0] != "ingress-nginx" {
		t.Errorf("AllowedNamespaces = %v, want [ingress-nginx]", spec.AllowedNamespaces)
	}
	if spec.AdminSubject.Kind != SubjectKindGroup || spec.AdminSubject.Name != "acme-admins" {
		t.Errorf("AdminSubject = %+v, unexpected", spec.AdminSubject)
	}
	// README: "an omitted podSecurityLevel in a file means no PodSecurity
	// labels at all, not the restricted default" -- unlike the --pod-security
	// flag, which defaults to restricted. This file has no podSecurityLevel
	// key at all.
	if spec.PodSecurityLevel != "" {
		t.Errorf("PodSecurityLevel = %q, want empty for a file that omits podSecurityLevel", spec.PodSecurityLevel)
	}

	if err := Validate(spec); err != nil {
		t.Fatalf("a spec loaded from a well-formed file should pass Validate, got: %v", err)
	}
}

func TestLoadSpecFile_PodSecurityLevelExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	const data = `
name: locked-down-corp
quota: {cpuRequest: "2", memoryRequest: 4Gi, cpuLimit: "4", memoryLimit: 8Gi, maxPods: "20"}
limitRange: {defaultCPURequest: 100m, defaultMemoryRequest: 128Mi, defaultCPULimit: 500m, defaultMemoryLimit: 512Mi}
podSecurityLevel: baseline
adminSubject: {kind: Group, name: locked-down-admins}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	spec, err := LoadSpecFile(path)
	if err != nil {
		t.Fatalf("LoadSpecFile: %v", err)
	}
	if spec.PodSecurityLevel != PodSecurityBaseline {
		t.Errorf("PodSecurityLevel = %q, want %q", spec.PodSecurityLevel, PodSecurityBaseline)
	}
	if err := Validate(spec); err != nil {
		t.Fatalf("expected a spec with an explicit podSecurityLevel to pass Validate, got: %v", err)
	}
}

func TestLoadSpecFile_ServiceAccountSubject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	const data = `
name: ci-corp
quota: {cpuRequest: "2", memoryRequest: 4Gi, cpuLimit: "4", memoryLimit: 8Gi, maxPods: "20"}
limitRange: {defaultCPURequest: 100m, defaultMemoryRequest: 128Mi, defaultCPULimit: 500m, defaultMemoryLimit: 512Mi}
adminSubject:
  kind: ServiceAccount
  name: ci-bot
  namespace: ci
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write spec file: %v", err)
	}

	spec, err := LoadSpecFile(path)
	if err != nil {
		t.Fatalf("LoadSpecFile: %v", err)
	}
	if spec.AdminSubject.Kind != SubjectKindServiceAccount || spec.AdminSubject.Namespace != "ci" {
		t.Errorf("AdminSubject = %+v, want ServiceAccount in namespace ci", spec.AdminSubject)
	}
	if err := Validate(spec); err != nil {
		t.Fatalf("expected a valid ServiceAccount spec to pass Validate, got: %v", err)
	}
}

func TestLoadSpecFile_MissingFile(t *testing.T) {
	_, err := LoadSpecFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error loading a nonexistent spec file")
	}
}

func TestLoadSpecFile_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("name: [this is not a valid spec :::"), 0o600); err != nil {
		t.Fatalf("write bad spec file: %v", err)
	}

	_, err := LoadSpecFile(path)
	if err == nil {
		t.Fatal("expected an error loading malformed YAML")
	}
}

// TestLoadSpecFile_ExampleFileMatchesDocs loads the repo's own
// examples/tenant.yaml, which README.md and PROGRESS.md point readers at as
// the canonical --file/-f format, to make sure that exact file still parses
// and validates against the real Spec shape rather than a stale sample.
func TestLoadSpecFile_ExampleFileMatchesDocs(t *testing.T) {
	spec, err := LoadSpecFile(filepath.Join("..", "..", "examples", "tenant.yaml"))
	if err != nil {
		t.Fatalf("LoadSpecFile(examples/tenant.yaml): %v", err)
	}
	if err := Validate(spec); err != nil {
		t.Fatalf("examples/tenant.yaml should be a valid spec, got: %v", err)
	}
	if spec.Name != "acme-corp" {
		t.Errorf("Name = %q, want %q (per README/PROGRESS examples)", spec.Name, "acme-corp")
	}
	if len(spec.AllowedNamespaces) != 1 || spec.AllowedNamespaces[0] != "ingress-nginx" {
		t.Errorf("AllowedNamespaces = %v, want [ingress-nginx]", spec.AllowedNamespaces)
	}
	if spec.AdminSubject.Kind != SubjectKindGroup || spec.AdminSubject.Name != "acme-admins" {
		t.Errorf("AdminSubject = %+v, unexpected", spec.AdminSubject)
	}
	if spec.PodSecurityLevel != PodSecurityRestricted {
		t.Errorf("PodSecurityLevel = %q, want %q (per examples/tenant.yaml)", spec.PodSecurityLevel, PodSecurityRestricted)
	}
}
