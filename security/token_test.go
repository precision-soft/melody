package security

import "testing"

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
