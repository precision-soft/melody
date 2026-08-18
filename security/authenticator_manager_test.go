package security

import (
    "errors"
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

/* a nil authenticator is refused at construction, naming its position: left in place it is called on the request path, outside any recovery */
func TestNewAuthenticatorManager_NilAuthenticatorPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAuthenticatorManager(
                &testAuthenticator{
                    supportsCallback: func(request httpcontract.Request) bool { return false },
                    authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                        return nil, nil
                    },
                },
                nil,
            )
        },
        "authenticator at index 1 is nil",
    )
}

/* a typed-nil authenticator passed the plain nil comparison and was called on the request path; the reflective guard moves the failure to the definition site */
func TestNewAuthenticatorManager_TypedNilAuthenticatorPanics(t *testing.T) {
    var typedNilAuthenticator *ApiKeyHeaderAuthenticator

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAuthenticatorManager(typedNilAuthenticator)
        },
        "authenticator at index 0 is nil",
    )
}

/* the manager owns the authenticator list it was built with */
func TestNewAuthenticatorManager_CopiesTheCallersAuthenticators(t *testing.T) {
    callerAuthenticators := []securitycontract.Authenticator{
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool { return true },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                return NewAuthenticatedToken("u1", []string{"ROLE_USER"}), nil
            },
        },
    }

    manager := NewAuthenticatorManager(callerAuthenticators...)

    callerAuthenticators[0] = &testAuthenticator{
        supportsCallback: func(request httpcontract.Request) bool { return false },
        authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
            return nil, nil
        },
    }

    token, usedAuthenticator, err := manager.Authenticate(newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == usedAuthenticator {
        t.Fatalf("expected the manager to keep the authenticator it was built with")
    }

    if false == token.IsAuthenticated() {
        t.Fatalf("expected the original authenticator to have run")
    }
}

/* The authenticator is the application's, and a nil pointer of its own token type is how "no user" gets
written by hand; boxed in the contract it is not equal to nil. */
func TestAuthenticatorManager_AuthenticateReadsATypedNilTokenAsAbsent(t *testing.T) {
    manager := NewAuthenticatorManager(
        &testAuthenticator{
            supportsCallback: func(request httpcontract.Request) bool {
                return true
            },
            authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                var unassignedToken *AuthenticatedToken

                return unassignedToken, nil
            },
        },
    )

    token, usedAuthenticator, err := manager.Authenticate(nil)

    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }

    if false == usedAuthenticator {
        t.Fatalf("expected the authenticator to be reported as used")
    }

    if _, isAnonymous := token.(*AnonymousToken); false == isAnonymous {
        t.Fatalf("expected the anonymous token, got %T", token)
    }
}
