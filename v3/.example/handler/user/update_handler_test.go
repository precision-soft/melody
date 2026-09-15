package user

import (
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
)

func TestProtectsAnotherAdminRefusesAPeer(t *testing.T) {
    if false == protectsAnotherAdmin("admin-1", administrator("admin-2")) {
        t.Fatal("an administrator reached a peer")
    }
}

func TestProtectsAnotherAdminAllowsTheActorTheirOwnAccount(t *testing.T) {
    if true == protectsAnotherAdmin("admin-1", administrator("admin-1")) {
        t.Fatal("an administrator was refused their own account")
    }
}

func TestProtectsAnotherAdminAllowsAnAccountBelow(t *testing.T) {
    if true == protectsAnotherAdmin("admin-1", editor("editor-1")) {
        t.Fatal("an administrator was refused an account below them")
    }
}

func TestApiUpdateHandlerKeepsTheRolesABodyDoesNotName(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"username":"editor-1"}`,
    )

    if nethttp.StatusOK != statusCode {
        t.Fatalf("the update answered %d: %s", statusCode, body)
    }

    roles := userRepository.storedRoles(t, "editor-1")
    if 2 != len(roles) || entity.RoleUser != roles[0] || entity.RoleEditor != roles[1] {
        t.Fatalf("the roles the body never named were rewritten to %v", roles)
    }
}

func TestApiUpdateHandlerReplacesTheRolesABodyNames(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"roles":["`+entity.RoleUser+`"]}`,
    )

    if nethttp.StatusOK != statusCode {
        t.Fatalf("the update answered %d: %s", statusCode, body)
    }

    roles := userRepository.storedRoles(t, "editor-1")
    if 1 != len(roles) || entity.RoleUser != roles[0] {
        t.Fatalf("the roles the body named were not stored, got %v", roles)
    }
}

func TestApiUpdateHandlerFallsBackToTheBaseRoleForAnEmptyListTheBodyNames(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"roles":[]}`,
    )

    if nethttp.StatusOK != statusCode {
        t.Fatalf("the update answered %d: %s", statusCode, body)
    }

    roles := userRepository.storedRoles(t, "editor-1")
    if 1 != len(roles) || entity.RoleUser != roles[0] {
        t.Fatalf("an explicitly empty list stored %v", roles)
    }
}

func TestApiUpdateHandlerRefusesARoleCarryingAComma(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"roles":["ROLE_X,`+entity.RoleAdmin+`"]}`,
    )

    if nethttp.StatusBadRequest != statusCode {
        t.Fatalf("the smuggled role answered %d: %s", statusCode, body)
    }

    if false == strings.Contains(body, "must not contain commas") {
        t.Fatalf("the refusal did not name the reason: %s", body)
    }

    roles := userRepository.storedRoles(t, "editor-1")
    if 2 != len(roles) || entity.RoleUser != roles[0] || entity.RoleEditor != roles[1] {
        t.Fatalf("the refused update reached the row anyway, leaving %v", roles)
    }
}

func TestRolesForUpdateKeepsWhatTheBodyDoesNotName(t *testing.T) {
    current := []string{entity.RoleUser, entity.RoleEditor}

    kept := rolesForUpdate(nil, current)

    if 2 != len(kept) || entity.RoleUser != kept[0] || entity.RoleEditor != kept[1] {
        t.Fatalf("the roles the body never named were rewritten to %v", kept)
    }
}

func TestRolesForUpdateReplacesWhatTheBodyNames(t *testing.T) {
    replaced := rolesForUpdate([]string{entity.RoleUser}, []string{entity.RoleUser, entity.RoleEditor})

    if 1 != len(replaced) || entity.RoleUser != replaced[0] {
        t.Fatalf("the roles the body named were not stored, got %v", replaced)
    }
}

func TestRolesForUpdateFallsBackToTheBaseRoleForAnEmptyListTheBodyNames(t *testing.T) {
    answered := rolesForUpdate([]string{}, []string{entity.RoleUser, entity.RoleEditor})

    if 1 != len(answered) || entity.RoleUser != answered[0] {
        t.Fatalf("an explicitly empty list answered %v", answered)
    }
}
