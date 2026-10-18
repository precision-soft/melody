package security

import (
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v3/.example/entity"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

func TestNewRoleHierarchy_ExpandsEachRoleToWhatItOpens(t *testing.T) {
    hierarchy := NewRoleHierarchy()

    expectations := map[string][]string{
        entity.RoleUser:   {entity.RoleUser},
        entity.RoleEditor: {entity.RoleEditor, entity.RoleUser},
        entity.RoleAdmin:  {entity.RoleAdmin, entity.RoleEditor, entity.RoleUser},
    }

    for role, expected := range expectations {
        if actual := hierarchy.ExpandRoles([]string{role}); false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("expected %s to expand to %v, got %v", role, expected, actual)
        }
    }
}

func TestViewerRoleList_AnswersTheExpandedRolesOfTheToken(t *testing.T) {
    runtimeInstance := runtimeCarryingToken(t, melodysecurity.NewAuthenticatedToken("user-2", []string{entity.RoleEditor}))

    expected := []string{entity.RoleEditor, entity.RoleUser}
    if actual := ViewerRoleList(runtimeInstance); false == reflect.DeepEqual(expected, actual) {
        t.Fatalf("expected the editor to be shown %v, got %v", expected, actual)
    }
}

func TestViewerRoleList_AnswersAnEmptyListForACallerWithoutAToken(t *testing.T) {
    actual := ViewerRoleList(runtimeWithoutSecurityContext(t))

    if nil == actual || 0 != len(actual) {
        t.Fatalf("expected an empty, non-nil list for an anonymous caller, got %#v", actual)
    }
}

func TestHoldsRole_ComparesTheExactSpelling(t *testing.T) {
    roleList := []string{entity.RoleEditor, entity.RoleUser}

    if false == HoldsRole(roleList, entity.RoleEditor) {
        t.Fatalf("expected the list to hold %s", entity.RoleEditor)
    }

    if true == HoldsRole(roleList, "role_editor") || true == HoldsRole(roleList, entity.RoleAdmin) {
        t.Fatalf("expected a different spelling and an absent role to be refused")
    }
}
