package tenant

import (
	"strings"
	"testing"
)

func validSpec() Spec {
	return Spec{
		Name:       "acme-corp",
		Quota:      DefaultQuota(),
		LimitRange: DefaultLimitRange(),
		AdminSubject: AdminSubject{
			Kind: SubjectKindGroup,
			Name: "acme-admins",
		},
	}
}

func TestValidate_ValidSpec(t *testing.T) {
	if err := Validate(validSpec()); err != nil {
		t.Fatalf("expected valid spec to pass, got error: %v", err)
	}
}

func TestValidate_Name(t *testing.T) {
	cases := []struct {
		name    string
		tenant  string
		wantErr bool
	}{
		{"valid simple", "acme", false},
		{"valid with hyphen", "acme-corp", false},
		{"valid single char", "a", false},
		{"valid max length 63", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"uppercase", "Acme", true},
		{"underscore", "acme_corp", true},
		{"leading hyphen", "-acme", true},
		{"trailing hyphen", "acme-", true},
		{"too long 64 chars", strings.Repeat("a", 64), true},
		{"spaces", "acme corp", true},
		{"dot", "acme.corp", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validSpec()
			s.Name = c.tenant
			err := Validate(s)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for name %q, got nil", c.tenant)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error for name %q, got: %v", c.tenant, err)
			}
		})
	}
}

func TestValidate_QuotaQuantities(t *testing.T) {
	fields := []struct {
		name  string
		apply func(*Spec, string)
	}{
		{"cpuRequest", func(s *Spec, v string) { s.Quota.CPURequest = v }},
		{"memoryRequest", func(s *Spec, v string) { s.Quota.MemoryRequest = v }},
		{"cpuLimit", func(s *Spec, v string) { s.Quota.CPULimit = v }},
		{"memoryLimit", func(s *Spec, v string) { s.Quota.MemoryLimit = v }},
		{"maxPods", func(s *Spec, v string) { s.Quota.MaxPods = v }},
	}

	for _, f := range fields {
		t.Run(f.name+"/empty", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for empty %s", f.name)
			}
		})
		t.Run(f.name+"/unparsable", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "not-a-quantity")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for unparsable %s", f.name)
			}
		})
		t.Run(f.name+"/negative", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "-5")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for negative %s (-5), but it passed Validate() -- "+
					"issue #6: a negative quota value parses fine but is rejected by the API "+
					"server at apply time, after the namespace has already been created", f.name)
			}
		})
		t.Run(f.name+"/zero is still valid", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "0")
			if err := Validate(s); err != nil {
				t.Fatalf("expected zero %s to remain valid, got: %v", f.name, err)
			}
		})
	}
}

func TestValidate_LimitRangeQuantities(t *testing.T) {
	fields := []struct {
		name  string
		apply func(*Spec, string)
	}{
		{"defaultCPURequest", func(s *Spec, v string) { s.LimitRange.DefaultCPURequest = v }},
		{"defaultMemoryRequest", func(s *Spec, v string) { s.LimitRange.DefaultMemoryRequest = v }},
		{"defaultCPULimit", func(s *Spec, v string) { s.LimitRange.DefaultCPULimit = v }},
		{"defaultMemoryLimit", func(s *Spec, v string) { s.LimitRange.DefaultMemoryLimit = v }},
	}

	for _, f := range fields {
		t.Run(f.name+"/empty", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for empty %s", f.name)
			}
		})
		t.Run(f.name+"/unparsable", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "nope")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for unparsable %s", f.name)
			}
		})
		t.Run(f.name+"/negative", func(t *testing.T) {
			s := validSpec()
			f.apply(&s, "-1Gi")
			if err := Validate(s); err == nil {
				t.Fatalf("expected error for negative %s (-1Gi), but it passed Validate() -- "+
					"issue #6: a negative quantity parses fine but is rejected by the API "+
					"server at apply time, after the namespace has already been created", f.name)
			}
		})
	}
}

func TestValidate_AdminSubject(t *testing.T) {
	t.Run("invalid kind", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject.Kind = SubjectKind("Robot")
		if err := Validate(s); err == nil {
			t.Fatal("expected error for invalid admin subject kind")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject.Name = ""
		if err := Validate(s); err == nil {
			t.Fatal("expected error for empty admin subject name")
		}
	})

	t.Run("service account without namespace", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject = AdminSubject{Kind: SubjectKindServiceAccount, Name: "ci-bot"}
		if err := Validate(s); err == nil {
			t.Fatal("expected error for ServiceAccount admin subject missing namespace")
		}
	})

	t.Run("service account with namespace is valid", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject = AdminSubject{Kind: SubjectKindServiceAccount, Name: "ci-bot", Namespace: "ci"}
		if err := Validate(s); err != nil {
			t.Fatalf("expected ServiceAccount admin subject with namespace to be valid, got: %v", err)
		}
	})

	t.Run("user kind does not require namespace", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject = AdminSubject{Kind: SubjectKindUser, Name: "alice"}
		if err := Validate(s); err != nil {
			t.Fatalf("expected User admin subject without namespace to be valid, got: %v", err)
		}
	})

	t.Run("group kind does not require namespace", func(t *testing.T) {
		s := validSpec()
		s.AdminSubject = AdminSubject{Kind: SubjectKindGroup, Name: "platform-admins"}
		if err := Validate(s); err != nil {
			t.Fatalf("expected Group admin subject without namespace to be valid, got: %v", err)
		}
	})
}

func TestValidate_AllowedNamespaces(t *testing.T) {
	t.Run("nil list is valid (default isolation)", func(t *testing.T) {
		s := validSpec()
		s.AllowedNamespaces = nil
		if err := Validate(s); err != nil {
			t.Fatalf("expected nil allowedNamespaces to be valid, got: %v", err)
		}
	})

	t.Run("valid entries pass", func(t *testing.T) {
		s := validSpec()
		s.AllowedNamespaces = []string{"ingress-nginx", "shared-tools"}
		if err := Validate(s); err != nil {
			t.Fatalf("expected valid allowedNamespaces to pass, got: %v", err)
		}
	})

	t.Run("invalid entry fails", func(t *testing.T) {
		s := validSpec()
		s.AllowedNamespaces = []string{"Not_Valid"}
		if err := Validate(s); err == nil {
			t.Fatal("expected error for an invalid entry in allowedNamespaces")
		}
	})

	t.Run("one bad entry among good ones still fails", func(t *testing.T) {
		s := validSpec()
		s.AllowedNamespaces = []string{"ingress-nginx", "Bad_NS"}
		if err := Validate(s); err == nil {
			t.Fatal("expected error when any entry in allowedNamespaces is invalid")
		}
	})
}

func TestValidate_PodSecurityLevel(t *testing.T) {
	cases := []struct {
		name    string
		level   PodSecurityLevel
		wantErr bool
	}{
		{"empty disables PSA labels", "", false},
		{"privileged", PodSecurityPrivileged, false},
		{"baseline", PodSecurityBaseline, false},
		{"restricted", PodSecurityRestricted, false},
		{"none is not a Spec-level value", PodSecurityLevel("none"), true},
		{"arbitrary string", PodSecurityLevel("super-locked-down"), true},
		{"wrong case", PodSecurityLevel("Restricted"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validSpec()
			s.PodSecurityLevel = c.level
			err := Validate(s)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for podSecurityLevel %q, got nil", c.level)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error for podSecurityLevel %q, got: %v", c.level, err)
			}
		})
	}
}

func TestDefaultQuota(t *testing.T) {
	q := DefaultQuota()
	if q.CPURequest != "2" || q.MemoryRequest != "4Gi" || q.CPULimit != "4" || q.MemoryLimit != "8Gi" || q.MaxPods != "20" {
		t.Fatalf("DefaultQuota() does not match documented defaults: %+v", q)
	}
}

func TestDefaultLimitRange(t *testing.T) {
	lr := DefaultLimitRange()
	if lr.DefaultCPURequest != "100m" || lr.DefaultMemoryRequest != "128Mi" || lr.DefaultCPULimit != "500m" || lr.DefaultMemoryLimit != "512Mi" {
		t.Fatalf("DefaultLimitRange() does not match documented defaults: %+v", lr)
	}
}
