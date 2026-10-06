package user

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
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
func TestRolesForUpdateLeavesTheRolesUnwrittenWhenTheBodyDoesNotNameThem(t *testing.T) {
    if kept := rolesForUpdate(nil); nil != kept {
        t.Fatalf("roles the body never named must not be written, got %v", kept)
    }
}

func TestRolesForUpdateReplacesWhatTheBodyNames(t *testing.T) {
    replaced := rolesForUpdate([]string{entity.RoleUser})

    if 1 != len(replaced) || entity.RoleUser != replaced[0] {
        t.Fatalf("the roles the body named were not stored, got %v", replaced)
    }
}

func TestRolesForUpdateFallsBackToTheBaseRoleForAnEmptyListTheBodyNames(t *testing.T) {
    answered := rolesForUpdate([]string{})

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

/* the four cells of the write-back: a stale entry of the account primed in the cache or not, a grant landed in the directory behind the cache or not. In every cell an admin who only renames the account leaves the password and the roles the directory holds. */
func TestApiUpdateHandlerWritesOnlyWhatTheBodyNamesOverTheDirectorysAccount(t *testing.T) {
    for _, primed := range []bool{true, false} {
        for _, granted := range []bool{true, false} {
            userRepository := newRecordingUserRepository(administrator("admin-1"), &entity.User{Id: "user-7", Username: "alice", Password: "H2", Roles: []string{entity.RoleUser}})
            cacheInstance := &valueCache{values: map[string]any{}}

            if true == primed {
                cacheInstance.values[service.CacheKeyUserById("user-7")] = &entity.User{Id: "user-7", Username: "alice", Password: "H1", Roles: []string{entity.RoleUser, entity.RoleAdmin}}
            }

            wantedRoles := []string{entity.RoleUser}
            if true == granted {
                if _, _, grantErr := userRepository.GrantRole(context.Background(), "user-7", entity.RoleEditor); nil != grantErr {
                    t.Fatalf("grant: %v", grantErr)
                }

                wantedRoles = []string{entity.RoleUser, entity.RoleEditor}
            }

            runtimeInstance := adminRuntimeOverCache(t, userRepository, cacheInstance, "admin-1", []string{entity.RoleAdmin})

            statusCode, body := callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/user-7/", map[string]string{"id": "user-7"}, `{"username":"alice-renamed"}`)
            if nethttp.StatusOK != statusCode {
                t.Fatalf("primed=%v granted=%v: the update answered %d: %s", primed, granted, statusCode, body)
            }

            stored := userRepository.stored(t, "user-7")
            if "H2" != stored.Password || "alice-renamed" != stored.Username || strings.Join(wantedRoles, ",") != strings.Join(stored.Roles, ",") {
                t.Fatalf("primed=%v granted=%v: the directory holds %+v; wanted H2, alice-renamed, %v", primed, granted, stored, wantedRoles)
            }
        }
    }
}

func TestApiUpdateHandlerRefusesAPeerAdministratorAndWritesNothing(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), administrator("admin-2"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/admin-2/", map[string]string{"id": "admin-2"}, `{"username":"demoted","roles":["ROLE_USER"]}`)
    if nethttp.StatusForbidden != statusCode {
        t.Fatalf("an update of a peer administrator answered %d: %s", statusCode, body)
    }

    stored := userRepository.stored(t, "admin-2")
    if "admin-2" != stored.Username || 2 != len(stored.Roles) {
        t.Fatalf("a refused update wrote %+v", stored)
    }

    statusCode, body = callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/admin-1/", map[string]string{"id": "admin-1"}, `{"username":"admin-one"}`)
    if nethttp.StatusOK != statusCode {
        t.Fatalf("an administrator's update of their own account answered %d: %s", statusCode, body)
    }
}

/* the peer decision is taken on the directory: an account the cache still holds as an editor, promoted behind it, is refused */
func TestApiUpdateHandlerDecidesThePeerRefusalOnTheDirectoryNotTheCache(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), administrator("admin-2"))
    cacheInstance := &valueCache{values: map[string]any{service.CacheKeyUserById("admin-2"): editor("admin-2")}}
    runtimeInstance := adminRuntimeOverCache(t, userRepository, cacheInstance, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/admin-2/", map[string]string{"id": "admin-2"}, `{"roles":["ROLE_USER"]}`)
    if nethttp.StatusForbidden != statusCode {
        t.Fatalf("a promoted peer the cache still holds as an editor answered %d: %s", statusCode, body)
    }
}

func TestApiUpdateDoor_AcceptsA72BytePasswordAndRefuses73(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})
    before := userRepository.stored(t, "editor-1").Password

    statusCode, body := callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/editor-1/", map[string]string{"id": "editor-1"},
        `{"password":"`+strings.Repeat("p", security.PasswordMaximumBytes+1)+`"}`)
    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, security.PasswordTooLongMessage) {
        t.Fatalf("the 73-byte password answered %d: %s", statusCode, body)
    }

    if before != userRepository.stored(t, "editor-1").Password {
        t.Fatal("the refused password was written anyway")
    }

    statusCode, body = callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/editor-1/", map[string]string{"id": "editor-1"},
        `{"password":"`+strings.Repeat("p", security.PasswordMaximumBytes)+`"}`)
    if nethttp.StatusOK != statusCode {
        t.Fatalf("the 72-byte password answered %d: %s", statusCode, body)
    }

    if before == userRepository.stored(t, "editor-1").Password {
        t.Fatal("the accepted password was not written")
    }
}

func TestApiUpdateDoor_AcceptsA255ByteUsernameAndRefuses256(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/editor-1/", map[string]string{"id": "editor-1"},
        `{"username":"`+strings.Repeat("a", 256)+`"}`)
    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "within 255 bytes") {
        t.Fatalf("the 256-byte username answered %d: %s", statusCode, body)
    }

    username := strings.Repeat("a", 255)
    statusCode, body = callDoor(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/editor-1/", map[string]string{"id": "editor-1"},
        `{"username":"`+username+`"}`)
    if nethttp.StatusOK != statusCode {
        t.Fatalf("the 255-byte username answered %d: %s", statusCode, body)
    }

    if username != userRepository.stored(t, "editor-1").Username {
        t.Fatalf("the accepted username was not written, the account holds %q", userRepository.stored(t, "editor-1").Username)
    }
}

/* the kernel bounds every body; a body past it is the client's too large a payload, not unreadable json */
func TestApiUpdateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    userRepository := newRecordingUserRepository(administrator("admin-1"), editor("editor-1"))
    runtimeInstance := adminRuntime(t, userRepository, "admin-1", []string{entity.RoleAdmin})

    statusCode, body := callDoorBoundedTo(t, runtimeInstance, ApiUpdateHandler(), nethttp.MethodPut, "/users/api/update/editor-1/", map[string]string{"id": "editor-1"}, `{"username":"bounded-account","password":"a-password"}`, 8)
    if nethttp.StatusRequestEntityTooLarge != statusCode {
        t.Fatalf("expected 413, got %d: %s", statusCode, body)
    }

    if _, exists, _ := userRepository.FindByUsername(context.Background(), "bounded-account"); true == exists {
        t.Fatal("expected nothing stored from a body never read")
    }
}
