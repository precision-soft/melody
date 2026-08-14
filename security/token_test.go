package security

import (
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
)

func TestNewToken_PanicsOnNil(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = NewToken(nil)
}

func TestNewToken_ReturnsSameWhenAlreadyWrapped(t *testing.T) {
    wrapped := NewToken(NewAnonymousToken())

    again := NewToken(wrapped)

    if wrapped != again {
        t.Fatalf("expected same instance")
    }
}

func TestToken_DelegatesToUnderlyingToken(t *testing.T) {
    user := NewAuthenticatedToken("u1", []string{"ROLE_A"})

    wrapped := NewToken(user)

    if false == wrapped.IsAuthenticated() {
        t.Fatalf("expected authenticated")
    }
    if "u1" != wrapped.UserIdentifier() {
        t.Fatalf("unexpected user identifier")
    }
    if 1 != len(wrapped.Roles()) {
        t.Fatalf("unexpected roles")
    }
    if "ROLE_A" != wrapped.Roles()[0] {
        t.Fatalf("unexpected role")
    }

    if user != wrapped.User() {
        t.Fatalf("unexpected user reference")
    }
}

/* rolelessTestToken answers a nil role list, which is what carries the wrapper's nil branch: without it the wrapper would hand back an empty slice and a caller could not tell "declared none" from "declared empty". */
type rolelessTestToken struct{}

func (instance *rolelessTestToken) UserIdentifier() string { return "u1" }
func (instance *rolelessTestToken) Roles() []string        { return nil }
func (instance *rolelessTestToken) IsAuthenticated() bool  { return true }

func TestToken_RolesAnswersACopy(t *testing.T) {
    token := NewToken(NewAuthenticatedToken("u1", []string{"ROLE_USER"}))

    readRoles := token.Roles()
    readRoles[0] = "ROLE_ADMIN"

    if "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the wrapper to hand out a copy, got %v", token.Roles())
    }
}

func TestToken_RolesKeepsANilRoleListNil(t *testing.T) {
    token := NewToken(&rolelessTestToken{})

    if nil != token.Roles() {
        t.Fatalf("expected a nil role list to stay nil through the wrapper, got %v", token.Roles())
    }
}

func TestNewToken_TypedNilPanics(t *testing.T) {
    var typedNilToken *AuthenticatedToken

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewToken(typedNilToken)
        },
        "can not create a security token from nil",
    )
}
