package user

import (
    "sort"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
)

func sortedRoles(roles []string) []string {
    sorted := append([]string{}, roles...)
    sort.Strings(sorted)

    return sorted
}

func TestNormalizeRolesDropsBlanksAndDuplicates(t *testing.T) {
    normalized := sortedRoles(normalizeRoles([]string{
        entity.RoleEditor,
        "  ",
        entity.RoleUser,
        entity.RoleEditor,
        "",
        "  " + entity.RoleUser + "  ",
    }))

    if 2 != len(normalized) {
        t.Fatalf("expected two roles, got %v", normalized)
    }

    if entity.RoleEditor != normalized[0] || entity.RoleUser != normalized[1] {
        t.Fatalf("unexpected roles: %v", normalized)
    }
}

/* An account is never left with nothing: an empty role list is what the access control reads as "no rule grants this", and the catch-all rule of the example guards every path behind ROLE_USER, so a user saved with none could sign in and reach nothing at all. */
func TestNormalizeRolesFallsBackToTheBaseRole(t *testing.T) {
    for name, roles := range map[string][]string{
        "no roles at all":  nil,
        "an empty list":    {},
        "only blank roles": {"", "   ", "\t"},
    } {
        t.Run(name, func(t *testing.T) {
            normalized := normalizeRoles(roles)

            if 1 != len(normalized) {
                t.Fatalf("expected exactly the base role, got %v", normalized)
            }

            if entity.RoleUser != normalized[0] {
                t.Fatalf("unexpected fallback: %v", normalized)
            }
        })
    }
}

/* The roles are trimmed but NOT folded, because the access control compares them exactly: a role saved as "role_admin" would grant nothing and read as though it did. */
func TestNormalizeRolesKeepsTheCaseItWasGiven(t *testing.T) {
    normalized := normalizeRoles([]string{" role_admin "})

    if 1 != len(normalized) || "role_admin" != normalized[0] {
        t.Fatalf("unexpected roles: %v", normalized)
    }
}
