package user

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

/* The order is part of the answer: the repository joins the list into one comma-separated column, so an answer built by ranging a map writes a different spelling of the same set on a share of the saves — and the audit trail, which compares the stored values, then records a change nobody asked for. A thousand runs is the sample the rate needs: at the measured 13 per cent for two roles, ten runs miss it a quarter of the time. */
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

/* the repository stores the role list comma-joined, so a role carrying a comma would come back as several roles on the next read — among them, possibly, an administrator nobody granted */
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

/* A file-backed session storage snapshots through json, so the role list comes back as []any: the copy of the helper this package holds must accept the same two spellings the token resolver accepts, or the door reading it answers an empty role list to a signed-in caller. */
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
