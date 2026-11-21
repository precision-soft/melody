package security

import (
    "errors"
    "testing"

    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestUnavailableToken_IsNotAuthenticatedAndCarriesItsCause(t *testing.T) {
    cause := errors.New("redis is down")
    token := NewUnavailableToken(cause)

    if true == token.IsAuthenticated() || "" != token.UserIdentifier() || 0 != len(token.Roles()) || 0 != len(token.Scope()) || 0 != len(token.Attributes()) {
        t.Fatalf("expected an empty, not authenticated token, got %+v", token)
    }

    var capability securitycontract.AuthenticationUnavailable = token
    if cause != capability.AuthenticationUnavailableCause() {
        t.Fatalf("expected the cause through the contract, got %v", capability.AuthenticationUnavailableCause())
    }

    readCause, unavailable := AuthenticationUnavailableCause(token)
    if false == unavailable || cause != readCause {
        t.Fatalf("expected the cause through the reader, got %v %v", readCause, unavailable)
    }

    var typedNil *UnavailableToken
    for name, other := range map[string]securitycontract.Token{
        "anonymous":     NewAnonymousToken(),
        "authenticated": NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil),
        "nil":           nil,
        "typed nil":     typedNil,
    } {
        if readCause, unavailable := AuthenticationUnavailableCause(other); true == unavailable || nil != readCause {
            t.Fatalf("expected %s to answer false, got %v %v", name, readCause, unavailable)
        }
    }
}

type nilCauseUnavailableToken struct {
    AnonymousToken
}

func (instance *nilCauseUnavailableToken) AuthenticationUnavailableCause() error { return nil }

func TestAuthenticationUnavailableCause_ATokenCarryingTheCapabilityWithANilCauseIsStillUnavailable(t *testing.T) {
    cause, unavailable := AuthenticationUnavailableCause(&nilCauseUnavailableToken{})
    if false == unavailable || false == errors.Is(cause, ErrAuthenticationUnavailable) {
        t.Fatalf("expected ErrAuthenticationUnavailable for a nil cause, got %v %v", cause, unavailable)
    }
}

func TestNewUnavailableToken_RefusesANilCause(t *testing.T) {
    var typedNil *unavailableTestError

    expectPanicNamingArgument(t, "NewUnavailableToken", "cause", func() { NewUnavailableToken(nil) })
    expectPanicNamingArgument(t, "NewUnavailableToken", "cause", func() { NewUnavailableToken(typedNil) })
}

type unavailableTestError struct{}

func (instance *unavailableTestError) Error() string { return "unavailable test error" }
