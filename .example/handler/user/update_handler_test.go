package user

import (
    "testing"

    "github.com/precision-soft/melody/.example/entity"
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
