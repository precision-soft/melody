package user

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestNormalizeRolesAnswersTheOrderItWasGiven(t *testing.T) {
    given := []string{entity.RoleEditor, entity.RoleAdmin, entity.RoleUser}
    wanted := strings.Join(given, ",")

    for run := 0; run < 1000; run++ {
        answered := strings.Join(normalizeRoles(given), ",")
        if wanted != answered {
            t.Fatalf("run %d answered %q, wanted %q", run, answered, wanted)
        }
    }
}

func TestNormalizeRolesDropsBlanksAndDuplicatesKeepingTheFirstPlace(t *testing.T) {
    normalized := normalizeRoles([]string{
        entity.RoleEditor,
        "  ",
        entity.RoleUser,
        entity.RoleEditor,
        "",
        "  " + entity.RoleUser + "  ",
    })

    if 2 != len(normalized) {
        t.Fatalf("expected two roles, got %v", normalized)
    }

    if entity.RoleEditor != normalized[0] || entity.RoleUser != normalized[1] {
        t.Fatalf("a duplicate moved a role out of the place it was first given: %v", normalized)
    }
}

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

func TestNormalizeRolesKeepsTheCaseItWasGiven(t *testing.T) {
    normalized := normalizeRoles([]string{" role_admin "})

    if 1 != len(normalized) || "role_admin" != normalized[0] {
        t.Fatalf("unexpected roles: %v", normalized)
    }
}

func TestRoleContainingCommaReportsTheOffendingRole(t *testing.T) {
    role, found := roleContainingComma([]string{entity.RoleUser, "ROLE_X," + entity.RoleAdmin})
    if false == found {
        t.Fatal("expected the compound role reported")
    }

    if "ROLE_X,"+entity.RoleAdmin != role {
        t.Fatalf("expected the offending role named, got %q", role)
    }
}

func TestRoleContainingCommaAcceptsPlainRoles(t *testing.T) {
    if _, found := roleContainingComma([]string{entity.RoleUser, entity.RoleAdmin}); true == found {
        t.Fatal("expected plain roles accepted")
    }
}

func TestGetStringSliceFromSessionAcceptsARestoredRoleList(t *testing.T) {
    sessionInstance := sessionCarrying(t, map[string]any{
        "roles": []any{entity.RoleUser, entity.RoleEditor},
    })

    roles, ok := getStringSliceFromSession(sessionInstance, "roles")
    if false == ok {
        t.Fatal("a restored role list was refused")
    }

    if 2 != len(roles) || entity.RoleUser != roles[0] || entity.RoleEditor != roles[1] {
        t.Fatalf("unexpected roles: %v", roles)
    }
}

func TestGetStringSliceFromSessionRefusesWhatIsNotARoleList(t *testing.T) {
    for name, value := range map[string]any{
        "a bare string":                    entity.RoleUser,
        "a restored list holding a number": []any{entity.RoleUser, 7},
    } {
        t.Run(name, func(t *testing.T) {
            if _, ok := getStringSliceFromSession(sessionCarrying(t, map[string]any{"roles": value}), "roles"); true == ok {
                t.Fatal("the value was accepted as a role list")
            }
        })
    }
}
