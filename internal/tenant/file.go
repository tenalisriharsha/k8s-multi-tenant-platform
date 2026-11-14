package tenant

import (
	"fmt"
	"os"

	sigsyaml "sigs.k8s.io/yaml"
)

// LoadSpecFile reads a declarative tenant Spec from a YAML (or JSON) file on
// disk, for GitOps-style config-file-driven tenant management. The file's
// keys match Spec's field names (camelCase), e.g.:
//
//	name: acme-corp
//	quota:
//	  cpuRequest: "2"
//	  memoryRequest: 4Gi
//	  cpuLimit: "4"
//	  memoryLimit: 8Gi
//	  maxPods: "20"
//	limitRange:
//	  defaultCPURequest: 100m
//	  defaultMemoryRequest: 128Mi
//	  defaultCPULimit: 500m
//	  defaultMemoryLimit: 512Mi
//	allowedNamespaces: [ingress-nginx]
//	podSecurityLevel: restricted
//	adminSubject:
//	  kind: Group
//	  name: acme-admins
//
// LoadSpecFile does not call Validate; callers should validate the
// returned Spec before building or applying it.
func LoadSpecFile(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("read spec file: %w", err)
	}

	var s Spec
	if err := sigsyaml.UnmarshalStrict(data, &s); err != nil {
		return Spec{}, fmt.Errorf("parse spec file %q: %w", path, err)
	}
	return s, nil
}
