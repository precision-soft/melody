package security

import (
    "testing"
)

func TestAccountRevision_MovesOnRolesAndPasswordAndIgnoresRoleOrder(t *testing.T) {
    revision := AccountRevision("user-1", "version-1", []string{"ROLE_USER", "ROLE_EDITOR"})

    if revision != AccountRevision("user-1", "version-1", []string{"ROLE_EDITOR", "ROLE_USER"}) {
        t.Fatal("expected the revision to ignore the order of the roles")
    }

    for name, moved := range map[string]string{
        "a role added":       AccountRevision("user-1", "version-1", []string{"ROLE_USER", "ROLE_EDITOR", "ROLE_ADMIN"}),
        "a role removed":     AccountRevision("user-1", "version-1", []string{"ROLE_USER"}),
        "the password":       AccountRevision("user-1", "version-2", []string{"ROLE_USER", "ROLE_EDITOR"}),
        "the account":        AccountRevision("user-2", "version-1", []string{"ROLE_USER", "ROLE_EDITOR"}),
        "the parts regrouped": AccountRevision("user-1version-1", "", []string{"ROLE_USER", "ROLE_EDITOR"}),
    } {
        if revision == moved {
            t.Errorf("expected the revision to move on %s", name)
        }
    }
}

func TestAccountRevision_LeavesTheCallersRolesInTheirOrder(t *testing.T) {
    roles := []string{"ROLE_USER", "ROLE_EDITOR"}
    AccountRevision("user-1", "version-1", roles)

    if "ROLE_USER" != roles[0] || "ROLE_EDITOR" != roles[1] {
        t.Fatalf("expected the caller's roles untouched, got %v", roles)
    }
}
