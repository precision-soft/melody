package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func newTokenSourceTestRuntime(t *testing.T) (runtimecontract.Runtime, eventcontract.EventDispatcher) {
    t.Helper()

    serviceContainer := container.NewContainer()
    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())

    registerErr := serviceContainer.Register(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    loggerRegisterErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )
    if nil != loggerRegisterErr {
        t.Fatalf("unexpected error: %v", loggerRegisterErr)
    }

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), dispatcher
}

func TestNewResolverTokenSource_NilResolverPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewResolverTokenSource(nil)
        },
        "token resolver is nil",
    )
}

func TestResolverTokenSource_NameIdentifiesTheSource(t *testing.T) {
    tokenSource := NewResolverTokenSource(
        func(request httpcontract.Request) securitycontract.Token {
            return nil
        },
    )

    if "tokenResolver" != tokenSource.Name() {
        t.Fatalf("expected the source to name itself tokenResolver, got %q", tokenSource.Name())
    }
}

func TestResolverTokenSource_ResolveAnswersAnonymousWhenTheResolverFindsNobody(t *testing.T) {
    runtimeInstance, _ := newTokenSourceTestRuntime(t)

    tokenSource := NewResolverTokenSource(
        func(request httpcontract.Request) securitycontract.Token {
            return nil
        },
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == token {
        t.Fatalf("expected an anonymous token rather than nil")
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected the fallback token to be unauthenticated")
    }
}

func TestResolverTokenSource_ResolveAnswersTheResolvedToken(t *testing.T) {
    runtimeInstance, _ := newTokenSourceTestRuntime(t)

    resolvedToken := NewAuthenticatedToken("u1", []string{"ROLE_USER"})

    tokenSource := NewResolverTokenSource(
        func(request httpcontract.Request) securitycontract.Token {
            return resolvedToken
        },
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if resolvedToken != token {
        t.Fatalf("expected the resolved token to be answered unchanged")
    }
}

func TestResolverTokenSource_ResolveHandsTheRequestToTheResolver(t *testing.T) {
    runtimeInstance, _ := newTokenSourceTestRuntime(t)

    request := newFirewallTestRequest("/some/path")
    seenRequest := httpcontract.Request(nil)

    tokenSource := NewResolverTokenSource(
        func(resolverRequest httpcontract.Request) securitycontract.Token {
            seenRequest = resolverRequest

            return nil
        },
    )

    _, err := tokenSource.Resolve(runtimeInstance, request)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if request != seenRequest {
        t.Fatalf("expected the resolver to be handed the request being resolved")
    }
}

func TestNewAuthenticatorTokenSource_NilManagerPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewAuthenticatorTokenSource(nil)
        },
        "authenticator manager is nil",
    )
}

func TestAuthenticatorTokenSource_NameIdentifiesTheSource(t *testing.T) {
    tokenSource := NewAuthenticatorTokenSource(NewAuthenticatorManager())

    if "authenticatorManager" != tokenSource.Name() {
        t.Fatalf("expected the source to name itself authenticatorManager, got %q", tokenSource.Name())
    }
}

func TestAuthenticatorTokenSource_ResolveAnswersAnonymousWhenNoAuthenticatorSupports(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return false },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    t.Fatalf("should not be called")

                    return nil, nil
                },
            },
        ),
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected an anonymous token")
    }

    if 0 != len(*dispatchedNameList) {
        t.Fatalf("expected no login event when no authenticator ran, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveEmitsLoginSuccess(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewAuthenticatedToken("u1", []string{"ROLE_USER"}), nil
                },
            },
        ),
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == token.IsAuthenticated() {
        t.Fatalf("expected an authenticated token")
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginSuccess != (*dispatchedNameList)[0] {
        t.Fatalf("expected exactly one login success event, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveDoesNotAnnounceAnAnonymousToken(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, nil
                },
            },
        ),
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected an anonymous token")
    }

    if 0 != len(*dispatchedNameList) {
        t.Fatalf("expected no login event for an anonymous outcome, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveEmitsLoginFailureAndKeepsTheError(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    authenticationErr := errors.New("invalid credentials")

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, authenticationErr
                },
            },
        ),
    )

    _, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if false == errors.Is(err, authenticationErr) {
        t.Fatalf("expected the authentication error to be handed back, got %v", err)
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected exactly one login failure event, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveKeepsTheAuthenticationErrorWhenTheFailureDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        0,
    )

    authenticationErr := errors.New("invalid credentials")

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, authenticationErr
                },
            },
        ),
    )

    _, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if false == errors.Is(err, authenticationErr) {
        t.Fatalf("expected the authentication error to survive as the cause, got %v", err)
    }
}

func TestAuthenticatorTokenSource_ResolveFailsWhenTheSuccessDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchErr := errors.New("event bus down")

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginSuccess,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return dispatchErr
        },
        0,
    )

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager(
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewAuthenticatedToken("u1", []string{"ROLE_USER"}), nil
                },
            },
        ),
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil == err {
        t.Fatalf("expected a failed success dispatch to refuse the resolution")
    }

    if nil != token {
        t.Fatalf("expected no token alongside the refusal, got %v", token)
    }
}

func recordSecurityLoginEvents(dispatcher eventcontract.EventDispatcher) *[]string {
    dispatchedNameList := make([]string, 0)

    eventNameList := []string{
        securitycontract.EventSecurityLoginSuccess,
        securitycontract.EventSecurityLoginFailure,
    }

    for _, eventName := range eventNameList {
        recordedName := eventName

        dispatcher.AddListener(
            eventName,
            func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                dispatchedNameList = append(dispatchedNameList, recordedName)

                return nil
            },
            0,
        )
    }

    return &dispatchedNameList
}

func TestResolverTokenSource_ResolveReadsATypedNilTokenAsAbsent(t *testing.T) {
    tokenSource := NewResolverTokenSource(
        func(request httpcontract.Request) securitycontract.Token {
            var unassignedToken *AuthenticatedToken

            return unassignedToken
        },
    )

    token, err := tokenSource.Resolve(nil, nil)

    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }

    if _, isAnonymous := token.(*AnonymousToken); false == isAnonymous {
        t.Fatalf("expected the anonymous token, got %T", token)
    }
}
