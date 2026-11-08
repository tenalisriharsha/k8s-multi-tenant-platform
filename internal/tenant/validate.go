package tenant

import (
	"fmt"
	"regexp"

	"k8s.io/apimachinery/pkg/api/resource"
)

// nameRE mirrors the Kubernetes RFC 1123 label/namespace naming rule:
// lowercase alphanumeric characters or '-', starting and ending with an
// alphanumeric character, 63 characters or fewer.
var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// Validate checks that a Spec is well-formed enough to render into
// Kubernetes objects. It does not contact a cluster.
func Validate(s Spec) error {
	if !nameRE.MatchString(s.Name) {
		return fmt.Errorf("invalid tenant name %q: must match %s (lowercase alphanumeric and '-', max 63 chars)", s.Name, nameRE.String())
	}

	if err := validateQuantity("quota.cpuRequest", s.Quota.CPURequest); err != nil {
		return err
	}
	if err := validateQuantity("quota.memoryRequest", s.Quota.MemoryRequest); err != nil {
		return err
	}
	if err := validateQuantity("quota.cpuLimit", s.Quota.CPULimit); err != nil {
		return err
	}
	if err := validateQuantity("quota.memoryLimit", s.Quota.MemoryLimit); err != nil {
		return err
	}
	if err := validateQuantity("quota.maxPods", s.Quota.MaxPods); err != nil {
		return err
	}

	if err := validateQuantity("limitRange.defaultCPURequest", s.LimitRange.DefaultCPURequest); err != nil {
		return err
	}
	if err := validateQuantity("limitRange.defaultMemoryRequest", s.LimitRange.DefaultMemoryRequest); err != nil {
		return err
	}
	if err := validateQuantity("limitRange.defaultCPULimit", s.LimitRange.DefaultCPULimit); err != nil {
		return err
	}
	if err := validateQuantity("limitRange.defaultMemoryLimit", s.LimitRange.DefaultMemoryLimit); err != nil {
		return err
	}

	for _, ns := range s.AllowedNamespaces {
		if !nameRE.MatchString(ns) {
			return fmt.Errorf("invalid entry %q in allowedNamespaces: must match %s (lowercase alphanumeric and '-', max 63 chars)", ns, nameRE.String())
		}
	}

	switch s.AdminSubject.Kind {
	case SubjectKindUser, SubjectKindGroup, SubjectKindServiceAccount:
	default:
		return fmt.Errorf("invalid admin subject kind %q: must be one of User, Group, ServiceAccount", s.AdminSubject.Kind)
	}
	if s.AdminSubject.Name == "" {
		return fmt.Errorf("admin subject name must not be empty")
	}
	if s.AdminSubject.Kind == SubjectKindServiceAccount && s.AdminSubject.Namespace == "" {
		return fmt.Errorf("admin subject namespace is required when kind is ServiceAccount")
	}

	return nil
}

func validateQuantity(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if _, err := resource.ParseQuantity(value); err != nil {
		return fmt.Errorf("%s: invalid quantity %q: %w", field, value, err)
	}
	return nil
}
