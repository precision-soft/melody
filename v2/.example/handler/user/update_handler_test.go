package user

import (
    "testing"

    "github.com/precision-soft/melody/v2/.example/entity"
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
func TestRolesForUpdateKeepsWhatTheBodyDoesNotName(t *testing.T) {
    current := []string{entity.RoleUser, entity.RoleEditor}

    kept := rolesForUpdate(nil, current)

    if 2 != len(kept) || entity.RoleUser != kept[0] || entity.RoleEditor != kept[1] {
        t.Fatalf("the roles the body never named were rewritten to %v", kept)
    }
}

/* the other half of the same rule: roles the body DOES name replace what the target held */
func TestRolesForUpdateReplacesWhatTheBodyNames(t *testing.T) {
    replaced := rolesForUpdate([]string{entity.RoleUser}, []string{entity.RoleUser, entity.RoleEditor})

    if 1 != len(replaced) || entity.RoleUser != replaced[0] {
        t.Fatalf("the roles the body named were not stored, got %v", replaced)
    }
}

/* a list sent EXPLICITLY empty is an opinion, and the base role is what normalizeRoles answers for it: an account is never left with none */
func TestRolesForUpdateFallsBackToTheBaseRoleForAnEmptyListTheBodyNames(t *testing.T) {
    answered := rolesForUpdate([]string{}, []string{entity.RoleUser, entity.RoleEditor})

    if 1 != len(answered) || entity.RoleUser != answered[0] {
        t.Fatalf("an explicitly empty list answered %v", answered)
    }
}
