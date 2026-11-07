package tenant

import (
	"bytes"
	"fmt"

	sigsyaml "sigs.k8s.io/yaml"
)

// RenderYAML marshals every object in a Bundle to a single multi-document
// YAML stream, in the order Kubernetes should apply them: namespace first,
// then quota/limits/network policy/RBAC.
func RenderYAML(b Bundle) ([]byte, error) {
	objs := []interface{}{b.Namespace, b.ResourceQuota, b.LimitRange}
	for _, np := range b.NetworkPolicies {
		objs = append(objs, np)
	}
	objs = append(objs, b.Role, b.RoleBinding)

	var buf bytes.Buffer
	for i, obj := range objs {
		if i > 0 {
			buf.WriteString("---\n")
		}
		doc, err := sigsyaml.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("marshal object %d: %w", i, err)
		}
		buf.Write(doc)
	}
	return buf.Bytes(), nil
}
