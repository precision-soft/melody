package user

import (
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/entity"
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

func TestApiReadDoorsEndWithTheRequest(t *testing.T) {
    userRepository := &slowUserRepository{recordingUserRepository: newRecordingUserRepository(administrator("admin-1"))}
    runtimeInstance := newCanceledRuntime(adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin}))

    if statusCode, body := callDoor(t, runtimeInstance, ApiReadAllHandler(), nethttp.MethodGet, "/users/api/", nil, ""); nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("the list answered %d for a request that had ended: %s", statusCode, body)
    }

    if statusCode, body := callDoor(t, runtimeInstance, ApiReadHandler(), nethttp.MethodGet, "/users/api/admin-1", map[string]string{"id": "admin-1"}, ""); nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("the read answered %d for a request that had ended: %s", statusCode, body)
    }
}

func TestApiReadDoor_FindsTheAccountUnderAPaddedIdentifier(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    if statusCode, body := callDoor(t, runtimeInstance, ApiReadHandler(), nethttp.MethodGet, "/users/api/%20admin-1%20", map[string]string{"id": " admin-1 "}, ""); nethttp.StatusOK != statusCode {
        t.Fatalf("expected the padded identifier found, got %d %s", statusCode, body)
    }
}
