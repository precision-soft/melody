package user

import (
    "errors"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/security"
)

func administrator(id string) *entity.User {
    return entity.NewUser(id, "admin", "hash", []string{entity.RoleUser, entity.RoleEditor, entity.RoleAdmin})
}

func editor(id string) *entity.User {
    return entity.NewUser(id, "editor", "hash", []string{entity.RoleUser, entity.RoleEditor})
}

/* An account that can grant roles is the one account whose holder must not be able to lock a colleague out or take their place quietly, so an administrator reaches their own account and everyone below them and stops at a peer. */
func TestProtectsAnotherAdminRefusesAPeer(t *testing.T) {
    if false == protectsAnotherAdmin("user-3", administrator("user-9")) {
        t.Fatalf("an administrator was allowed to reach another administrator")
    }
}

func TestProtectsAnotherAdminAllowsTheActorTheirOwnAccount(t *testing.T) {
    if true == protectsAnotherAdmin("user-3", administrator("user-3")) {
        t.Fatalf("an administrator was refused their own account")
    }
}

func TestProtectsAnotherAdminAllowsAnAccountBelow(t *testing.T) {
    if true == protectsAnotherAdmin("user-3", editor("user-9")) {
        t.Fatalf("an administrator was refused an account that is not an administrator")
    }
}

/* An actor the security context could not name is an empty identifier, which equals no administrator's id, so the peer stays protected rather than becoming reachable by anyone unnamed. */
func TestProtectsAnotherAdminRefusesAnUnnamedActor(t *testing.T) {
    if false == protectsAnotherAdmin("", administrator("user-9")) {
        t.Fatalf("an unnamed actor reached an administrator")
    }
}

func TestProtectsAnotherAdminToleratesAMissingTarget(t *testing.T) {
    if true == protectsAnotherAdmin("user-3", nil) {
        t.Fatalf("a missing target was reported as a protected administrator")
    }
}

func TestHasRoleAnswersTheExactRole(t *testing.T) {
    roles := []string{entity.RoleUser, entity.RoleEditor}

    if false == hasRole(roles, entity.RoleEditor) {
        t.Fatalf("a role that is present was reported absent")
    }

    if true == hasRole(roles, entity.RoleAdmin) {
        t.Fatalf("a role that is absent was reported present")
    }

    if true == hasRole(roles, "role_editor") {
        t.Fatalf("the comparison ignores case, so a role name matches a spelling nobody granted")
    }

    if true == hasRole(nil, entity.RoleUser) {
        t.Fatalf("an account with no roles was granted one")
    }
}

/* An update names the fields it changes: an omitted username and an omitted password are kept, and roles were the one field an omission REMOVED — the target came back holding the base role alone, an administrator editing their own account included. */
func TestRolesForUpdateLeavesTheRolesUnwrittenWhenTheBodyDoesNotNameThem(t *testing.T) {
    if kept := rolesForUpdate(nil); nil != kept {
        t.Fatalf("roles the body never named must not be written, got %v", kept)
    }
}

/* the other half of the same rule: roles the body DOES name replace what the target held */
func TestRolesForUpdateReplacesWhatTheBodyNames(t *testing.T) {
    replaced := rolesForUpdate([]string{entity.RoleUser})

    if 1 != len(replaced) || entity.RoleUser != replaced[0] {
        t.Fatalf("the roles the body named were not stored, got %v", replaced)
    }
}

/* a list sent EXPLICITLY empty is an opinion, and the base role is what normalizeRoles answers for it: an account is never left with none */
func TestRolesForUpdateFallsBackToTheBaseRoleForAnEmptyListTheBodyNames(t *testing.T) {
    answered := rolesForUpdate([]string{})

    if 1 != len(answered) || entity.RoleUser != answered[0] {
        t.Fatalf("an explicitly empty list answered %v", answered)
    }
}

func TestRefusingAnotherAdminRefusesAPeerAndAdmitsTheActorAndAnAccountBelow(t *testing.T) {
    guard := refusingAnotherAdmin("admin-1")

    if refusal := guard(administrator("admin-2")); false == errors.Is(refusal, errAnotherAdmin) {
        t.Fatalf("a peer administrator was admitted: %v", refusal)
    }

    if refusal := guard(administrator("admin-1")); nil != refusal {
        t.Fatalf("the actor's own account was refused: %v", refusal)
    }

    if refusal := guard(editor("editor-1")); nil != refusal {
        t.Fatalf("an account below was refused: %v", refusal)
    }
}

func TestApiUpdateDoor_AcceptsA72BytePasswordAndRefuses73(t *testing.T) {
    fixture := newUserDoorFixture(t)
    before := fixture.stored(t, "user-2").Password

    status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPut, `{"password":"`+strings.Repeat("p", security.PasswordMaximumBytes+1)+`"}`, map[string]string{"id": "user-2"})
    if nethttp.StatusBadRequest != status || false == strings.Contains(body, "72 bytes") {
        t.Fatalf("the 73-byte password answered %d: %s", status, body)
    }

    if before != fixture.stored(t, "user-2").Password {
        t.Fatal("the refused password was written anyway")
    }

    status, body = fixture.call(t, ApiUpdateHandler(), nethttp.MethodPut, `{"password":"`+strings.Repeat("p", security.PasswordMaximumBytes)+`"}`, map[string]string{"id": "user-2"})
    if nethttp.StatusOK != status {
        t.Fatalf("the 72-byte password answered %d: %s", status, body)
    }

    if before == fixture.stored(t, "user-2").Password {
        t.Fatal("the accepted password was not written")
    }
}

func TestApiUpdateDoor_AcceptsA255ByteUsernameAndRefuses256(t *testing.T) {
    fixture := newUserDoorFixture(t)

    status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPut, `{"username":"`+strings.Repeat("a", 256)+`"}`, map[string]string{"id": "user-2"})
    if nethttp.StatusBadRequest != status || false == strings.Contains(body, "within 255 bytes") {
        t.Fatalf("the 256-byte username answered %d: %s", status, body)
    }

    username := strings.Repeat("a", 255)
    status, body = fixture.call(t, ApiUpdateHandler(), nethttp.MethodPut, `{"username":"`+username+`"}`, map[string]string{"id": "user-2"})
    if nethttp.StatusOK != status || username != fixture.stored(t, "user-2").Username {
        t.Fatalf("the 255-byte username answered %d: %s", status, body)
    }
}

func TestApiUpdateDoor_AnswersABodyPastTheLimit413(t *testing.T) {
    fixture := newUserDoorFixture(t)
    fixture.bodyLimit = 8

    if status, body := fixture.call(t, ApiUpdateHandler(), nethttp.MethodPut, `{"username":"bounded-account","password":"a-password"}`, map[string]string{"id": "user-2"}); nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected 413, got %d %s", status, body)
    }

    if true == fixture.holds(t, "bounded-account") {
        t.Fatal("expected nothing stored from a body never read")
    }
}
