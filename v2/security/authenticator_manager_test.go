package security

import (
    "errors"
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

type testAuthenticator struct {
    supportsCallback     func(request httpcontract.Request) bool
    authenticateCallback func(request httpcontract.Request) (securitycontract.Token, error)
}

func (instance *testAuthenticator) Supports(request httpcontract.Request) bool {
    return instance.supportsCallback(request)
}

func (instance *testAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    return instance.authenticateCallback(request)
}

func TestAuthenticatorManager_ReturnsAnonymousWhenNoAuthenticatorSupports(t *testing.T) {
    manager := NewAuthenticatorManager(
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return false },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                t.Fatalf("should not be called")
                return nil, nil
            },
        },
    )

    request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

    token, _, err := manager.Authenticate(request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestAuthenticatorManager_ReturnsErrorWhenAuthenticatorErrors(t *testing.T) {
    expected := errors.New("auth error")

    manager := NewAuthenticatorManager(
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return true },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                return nil, expected
            },
        },
    )

    request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

    _, _, err := manager.Authenticate(request)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestAuthenticatorManager_ReturnsAnonymousWhenAuthenticatorReturnsNilToken(t *testing.T) {
    manager := NewAuthenticatorManager(
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return true },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                return nil, nil
            },
        },
    )

    request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

    token, _, err := manager.Authenticate(request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestAuthenticatorManager_FirstSupportingAuthenticatorWins(t *testing.T) {
    callsFirst := 0
    callsSecond := 0

    manager := NewAuthenticatorManager(
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return true },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                callsFirst++
                return NewAuthenticatedToken("u1", []string{"ROLE_A"}), nil
            },
        },
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return true },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                callsSecond++
                return NewAuthenticatedToken("u2", []string{"ROLE_B"}), nil
            },
        },
    )

    request := newSecurityTestRequest(nethttp.MethodGet, "/x", map[string]string{}, nil)

    token, _, err := manager.Authenticate(request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == token.IsAuthenticated() {
        t.Fatalf("expected authenticated token")
    }
    if "u1" != token.UserIdentifier() {
        t.Fatalf("unexpected user identifier")
    }

    if 1 != callsFirst {
        t.Fatalf("expected first authenticator to be called once")
    }
    if 0 != callsSecond {
        t.Fatalf("expected second authenticator to not be called")
    }
}

var _ securitycontract.Authenticator = (*testAuthenticator)(nil)
