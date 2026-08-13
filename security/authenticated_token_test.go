package security

import (
    "testing"
)

func TestAuthenticatedToken_CarriesItsIdentityAndRoles(t *testing.T) {
    token := NewAuthenticatedToken("u1", []string{"ROLE_USER"})

    if false == token.IsAuthenticated() {
        t.Fatalf("expected the token to be authenticated")
    }

    if "u1" != token.UserIdentifier() {
        t.Fatalf("expected the user identifier to be carried, got %q", token.UserIdentifier())
    }

    if 1 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the roles to be carried, got %v", token.Roles())
    }
}

/* the token owns its roles on both sides: a caller that edits the slice it passed in, or the slice it read back, must not be able to grant itself a role that the voters will then honour */
func TestAuthenticatedToken_OwnsItsRoles(t *testing.T) {
    callerRoles := []string{"ROLE_USER"}

    token := NewAuthenticatedToken("u1", callerRoles)

    callerRoles[0] = "ROLE_ADMIN"

    if "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the token to keep its own copy of the roles it was built with, got %v", token.Roles())
    }

    readRoles := token.Roles()
    readRoles[0] = "ROLE_ADMIN"

    if "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the token to hand out a copy of its roles, got %v", token.Roles())
    }
}

/* an absent role list stays absent rather than becoming an empty one: nil in, nil out, so a caller can tell "no roles declared" from "declared empty" */
func TestAuthenticatedToken_WithoutRolesAnswersAnEmptyList(t *testing.T) {
    token := NewAuthenticatedToken("u1", nil)

    roles := token.Roles()
    if nil == roles {
        t.Fatalf("expected an empty role list rather than nil")
    }

    if 0 != len(roles) {
        t.Fatalf("expected no roles, got %v", roles)
    }
}

/* the zero value is reachable from outside the package — the type is exported and its fields are not required — and it answers a nil role list rather than dereferencing. Pinned so the nil branch inside Roles stays honest about who can reach it. */
func TestAuthenticatedToken_ZeroValueAnswersNoRoles(t *testing.T) {
    token := &AuthenticatedToken{}

    if false == token.IsAuthenticated() {
        t.Fatalf("expected the zero value to still read as authenticated, which is what the type means")
    }

    if "" != token.UserIdentifier() {
        t.Fatalf("expected no user identifier on the zero value, got %q", token.UserIdentifier())
    }

    if nil != token.Roles() {
        t.Fatalf("expected the zero value to answer a nil role list, got %v", token.Roles())
    }
}

/* the token answers its own twin under another role set, which is what the role hierarchy voter asks of it instead of rebuilding one: the receiver keeps the roles it was built with, because the voter holds the caller's token while the delegate may keep the twin */
func TestAuthenticatedToken_WithRolesAnswersATwinAndLeavesTheReceiver(t *testing.T) {
    token := NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"})

    twin := token.WithRoles([]string{"ROLE_ADMIN", "ROLE_USER"})

    if 2 != len(twin.Roles()) {
        t.Fatalf("expected the twin to carry the new role set, got %v", twin.Roles())
    }

    if "u1" != twin.UserIdentifier() {
        t.Fatalf("expected the twin to keep the identifier, got %q", twin.UserIdentifier())
    }

    if 1 != len(token.Roles()) || "ROLE_ADMIN" != token.Roles()[0] {
        t.Fatalf("expected the receiver to keep its own roles, got %v", token.Roles())
    }

    if _, isAuthenticatedToken := twin.(*AuthenticatedToken); false == isAuthenticatedToken {
        t.Fatalf("expected the twin to keep the token's own type, got %T", twin)
    }
}
