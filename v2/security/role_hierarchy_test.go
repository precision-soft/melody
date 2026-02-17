package security

import "testing"

func TestRoleHierarchy_ExpandRoles_DeduplicatesAndSorts(t *testing.T) {
	hierarchy := NewRoleHierarchy(
		map[string][]string{
			"ROLE_ADMIN": {
				"ROLE_USER",
				"ROLE_AUDIT",
			},
			"ROLE_SUPER_ADMIN": {
				"ROLE_ADMIN",
				"ROLE_USER",
			},
		},
	)

	expanded := hierarchy.ExpandRoles(
		[]string{
			"ROLE_SUPER_ADMIN",
		},
	)

	expected := map[string]bool{
		"ROLE_SUPER_ADMIN": true,
		"ROLE_ADMIN":       true,
		"ROLE_USER":        true,
		"ROLE_AUDIT":       true,
	}

	for _, role := range expanded {
		_, ok := expected[role]
		if false == ok {
			t.Fatalf("unexpected role: %s", role)
		}
		delete(expected, role)
	}

	if 0 != len(expected) {
		t.Fatalf("expected all roles to be present, missing: %v", expected)
	}
}
