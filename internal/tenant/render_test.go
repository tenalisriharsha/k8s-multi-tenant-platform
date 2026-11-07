package tenant

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

func TestRenderYAML_DocumentOrderAndCount(t *testing.T) {
	s := validSpec()
	b := Build(s)

	out, err := RenderYAML(b)
	if err != nil {
		t.Fatalf("RenderYAML returned error: %v", err)
	}

	docs := splitYAMLDocs(string(out))
	wantKinds := []string{
		"Namespace", "ResourceQuota", "LimitRange",
		"NetworkPolicy", "NetworkPolicy", "NetworkPolicy",
		"Role", "RoleBinding",
	}
	if len(docs) != len(wantKinds) {
		t.Fatalf("expected %d YAML documents, got %d:\n%s", len(wantKinds), len(docs), out)
	}

	for i, doc := range docs {
		var tm metav1.TypeMeta
		if err := sigsyaml.Unmarshal([]byte(doc), &tm); err != nil {
			t.Fatalf("doc %d did not unmarshal as a Kubernetes object: %v\n%s", i, err, doc)
		}
		if tm.Kind != wantKinds[i] {
			t.Errorf("doc %d kind = %q, want %q", i, tm.Kind, wantKinds[i])
		}
		if tm.APIVersion == "" {
			t.Errorf("doc %d (%s) missing apiVersion", i, tm.Kind)
		}
	}
}

func TestRenderYAML_ValidYAMLPerDocument(t *testing.T) {
	s := validSpec()
	b := Build(s)

	out, err := RenderYAML(b)
	if err != nil {
		t.Fatalf("RenderYAML returned error: %v", err)
	}

	for i, doc := range splitYAMLDocs(string(out)) {
		var generic map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &generic); err != nil {
			t.Fatalf("doc %d is not valid YAML: %v", i, err)
		}
		if _, ok := generic["metadata"]; !ok {
			t.Errorf("doc %d has no metadata field", i)
		}
	}
}

func TestRenderYAML_IncludesTenantName(t *testing.T) {
	s := validSpec()
	s.Name = "widgets-inc"
	b := Build(s)

	out, err := RenderYAML(b)
	if err != nil {
		t.Fatalf("RenderYAML returned error: %v", err)
	}
	if !strings.Contains(string(out), "widgets-inc") {
		t.Errorf("rendered YAML does not mention tenant name %q", s.Name)
	}
}

// splitYAMLDocs mirrors the "---\n" document separator RenderYAML writes
// between objects, trimming the leading separator written before every
// document but the first.
func splitYAMLDocs(s string) []string {
	parts := strings.Split(s, "---\n")
	var docs []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			docs = append(docs, p)
		}
	}
	return docs
}
