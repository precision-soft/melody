package security

import (
    "reflect"
    "testing"
)

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

func TestRoleHierarchy_ExpandRolesTerminatesOnACycle(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_A": {"ROLE_B"},
            "ROLE_B": {"ROLE_A"},
        },
    )

    expanded := hierarchy.ExpandRoles([]string{"ROLE_A"})

    if false == reflect.DeepEqual([]string{"ROLE_A", "ROLE_B"}, expanded) {
        t.Fatalf("expected the cycle to expand to both roles exactly once, got %v", expanded)
    }
}

func TestRoleHierarchy_ExpandRolesTerminatesOnALongerCycle(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_A": {"ROLE_B"},
            "ROLE_B": {"ROLE_C"},
            "ROLE_C": {"ROLE_A"},
        },
    )

    expanded := hierarchy.ExpandRoles([]string{"ROLE_C"})

    if false == reflect.DeepEqual([]string{"ROLE_A", "ROLE_B", "ROLE_C"}, expanded) {
        t.Fatalf("expected the cycle to expand to all three roles exactly once, got %v", expanded)
    }
}

func TestRoleHierarchy_ExpandRolesDropsEmptyRoles(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_A": {"", "ROLE_B"},
        },
    )

    expanded := hierarchy.ExpandRoles([]string{"", "ROLE_A", ""})

    if false == reflect.DeepEqual([]string{"ROLE_A", "ROLE_B"}, expanded) {
        t.Fatalf("expected empty roles to be dropped, got %v", expanded)
    }
}

func TestRoleHierarchy_ExpandRolesDeduplicatesTheInput(t *testing.T) {
    hierarchy := NewRoleHierarchy(nil)

    expanded := hierarchy.ExpandRoles([]string{"ROLE_A", "ROLE_A"})

    if false == reflect.DeepEqual([]string{"ROLE_A"}, expanded) {
        t.Fatalf("expected the repeated role to appear once, got %v", expanded)
    }
}

func TestRoleHierarchy_ExpandRolesWithoutAnyInheritance(t *testing.T) {
    hierarchy := NewRoleHierarchy(nil)

    expanded := hierarchy.ExpandRoles([]string{"ROLE_A"})
    if false == reflect.DeepEqual([]string{"ROLE_A"}, expanded) {
        t.Fatalf("expected the role to pass through, got %v", expanded)
    }

    empty := hierarchy.ExpandRoles(nil)
    if 0 != len(empty) {
        t.Fatalf("expected no roles, got %v", empty)
    }
}

func TestNewRoleHierarchy_CopiesTheCallersMap(t *testing.T) {
    inheritedRolesByRole := map[string][]string{
        "ROLE_ADMIN": {"ROLE_USER"},
    }

    hierarchy := NewRoleHierarchy(inheritedRolesByRole)

    inheritedRolesByRole["ROLE_ADMIN"][0] = "ROLE_SUPER_ADMIN"
    inheritedRolesByRole["ROLE_GUEST"] = []string{"ROLE_ADMIN"}

    expanded := hierarchy.ExpandRoles([]string{"ROLE_ADMIN"})
    if false == reflect.DeepEqual([]string{"ROLE_ADMIN", "ROLE_USER"}, expanded) {
        t.Fatalf("expected the hierarchy to keep its own copy, got %v", expanded)
    }
}
