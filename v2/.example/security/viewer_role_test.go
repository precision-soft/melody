package security

import (
    "context"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodysecurity "github.com/precision-soft/melody/v2/security"
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

/* a token carries the roles the account was given, not the ones they imply, so the viewer's list is expanded through the hierarchy: an editor is shown ROLE_USER and an admin ROLE_EDITOR and ROLE_USER beside the role it holds */
func TestViewerRoleListOfToken_AnswersTheExpandedRolesOfTheToken(t *testing.T) {
    for role, expected := range map[string][]string{
        entity.RoleEditor: {entity.RoleEditor, entity.RoleUser},
        entity.RoleAdmin:  {entity.RoleAdmin, entity.RoleEditor, entity.RoleUser},
    } {
        token := melodysecurity.NewAuthenticatedToken("user-2", []string{role})
        if actual := viewerRoleListOfToken(token); false == reflect.DeepEqual(expected, actual) {
            t.Fatalf("expected %s to be shown %v, got %v", role, expected, actual)
        }
    }
}

func TestViewerRoleList_AnswersAnEmptyListForACallerWithoutAToken(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() {
        _ = containerInstance.Close()
    })

    actual := ViewerRoleList(melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance))

    if nil == actual || 0 != len(actual) {
        t.Fatalf("expected an empty, non-nil list for an anonymous caller, got %#v", actual)
    }

    if fromNil := viewerRoleListOfToken(nil); nil == fromNil || 0 != len(fromNil) {
        t.Fatalf("expected an empty, non-nil list for a nil token, got %#v", fromNil)
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
