package tenant

import (
	"bytes"
	"fmt"
	"strings"

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

// diffLines renders an unordered, line-level diff between two YAML byte
// streams: a line present only in current is prefixed "- ", a line present
// only in desired is prefixed "+ ", and a line present in both (regardless
// of position) is printed unprefixed as context. This is intentionally not
// a positional diff algorithm (no LCS, no hunk headers) — it is good enough
// to surface which fields drifted between a live tenant and its declared
// Spec without pulling in an external diff dependency.
func diffLines(current, desired []byte) string {
	curLines := splitNonEmptyLines(current)
	desLines := splitNonEmptyLines(desired)

	curCount := map[string]int{}
	for _, l := range curLines {
		curCount[l]++
	}
	desCount := map[string]int{}
	for _, l := range desLines {
		desCount[l]++
	}

	var b strings.Builder
	seenInCur := map[string]int{}
	for _, l := range curLines {
		seenInCur[l]++
		if seenInCur[l] <= desCount[l] {
			fmt.Fprintf(&b, "  %s\n", l)
		} else {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	seenInDes := map[string]int{}
	for _, l := range desLines {
		seenInDes[l]++
		if seenInDes[l] > curCount[l] {
			fmt.Fprintf(&b, "+ %s\n", l)
		}
	}
	return b.String()
}

func splitNonEmptyLines(b []byte) []string {
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
