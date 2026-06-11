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

/** @info scope and attribute defensive copy */

type sharedMapToken struct {
    scope      map[string]any
    attributes map[string]any
}

func (instance *sharedMapToken) IsAuthenticated() bool { return true }

func (instance *sharedMapToken) UserIdentifier() string { return "u1" }

func (instance *sharedMapToken) Roles() []string { return []string{"ROLE_A"} }

func (instance *sharedMapToken) Scope() map[string]any { return instance.scope }

func (instance *sharedMapToken) Attributes() map[string]any { return instance.attributes }

func TestToken_ScopeAndAttributesAreCopied(t *testing.T) {
    user := &sharedMapToken{
        scope:      map[string]any{"tenant": "acme"},
        attributes: map[string]any{"department": "engineering"},
    }

    wrapped := NewToken(user)

    returnedScope := wrapped.Scope()
    returnedScope["tenant"] = "evil"
    returnedScope["injected"] = true

    returnedAttributes := wrapped.Attributes()
    returnedAttributes["department"] = "evil"

    if "acme" != user.scope["tenant"] {
        t.Fatalf("mutating the returned Scope corrupted the underlying token scope")
    }
    if _, injected := user.scope["injected"]; true == injected {
        t.Fatalf("mutating the returned Scope injected a key into the underlying token scope")
    }
    if "engineering" != user.attributes["department"] {
        t.Fatalf("mutating the returned Attributes corrupted the underlying token attributes")
    }
}
