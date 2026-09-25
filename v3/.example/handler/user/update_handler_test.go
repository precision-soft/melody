package user

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
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

/* An update names the fields it changes: an omitted username and an omitted password are kept, and omitted roles are kept too rather than falling back to the base role, an administrator editing their own account included. */
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

/* the other half of the same rule: roles the body DOES name replace what the target held */
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

/* a list sent EXPLICITLY empty is an opinion, and normalizeRoles answers the base role for it: an account is never left with none, because the example's catch-all rule guards every path behind ROLE_USER */
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

/* the repository joins the role list into one comma-separated column, so a role carrying a comma comes back as SEVERAL roles on the next read — the update door is where that is refused, before the row lands */
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

/* the same rule, at the door on this major */
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

/* a rename the unique index refused after the door's check had passed is the caller's 400, the same answer
   the check gives, not a 500 of a failed write */
func TestApiUpdateHandlerAnswersTheIndexsRefusalOfATakenUsernameAs400(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    userRepository.refuseWrites(fmt.Errorf("audited update failed: %w", repository.ErrUsernameAlreadyExists))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"username":"renamed"}`,
    )

    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "username already exists") {
        t.Fatalf("the index's refusal answered %d: %s, wanted 400 username already exists", statusCode, body)
    }
}

/* the same refusal at the update door: a misspelt role is refused, and the row keeps its roles */
func TestApiUpdateHandlerRefusesARoleTheApplicationDoesNotKnow(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"roles":["ROLE_ADMIM"]}`,
    )

    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "ROLE_ADMIM is not one this application knows") {
        t.Fatalf("the misspelt role answered %d: %s", statusCode, body)
    }

    roles := userRepository.storedRoles(t, "editor-1")
    if 2 != len(roles) || entity.RoleUser != roles[0] || entity.RoleEditor != roles[1] {
        t.Fatalf("the refused update reached the row anyway, leaving %v", roles)
    }
}

/* the twin of the create door's: a write that failed for any reason but the taken username is the server's fault and answers 500 */
func TestApiUpdateHandlerAnswersAnyOtherFailedWriteAs500(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    userRepository.refuseWrites(errors.New("audited update failed"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, _ := callDoor(
        t,
        runtimeInstance,
        ApiUpdateHandler(),
        nethttp.MethodPut,
        "/users/api/update/editor-1/",
        map[string]string{"id": "editor-1"},
        `{"username":"renamed"}`,
    )

    if nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("a failed write answered %d, wanted 500", statusCode)
    }
}
