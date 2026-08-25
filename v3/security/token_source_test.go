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

/* newTokenSourceTestRuntime builds a runtime whose container carries the event dispatcher the authenticator token source reaches for, plus the logger the dispatcher's own failure path needs. The dispatcher is handed back so a test can plant a listener on it. */
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

/* a resolver that finds nobody answers an anonymous token rather than a nil one: the listener that stores the context would otherwise carry nil into the request, and every downstream nil check would have to repeat the decision */
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

/* the resolver reads the request it is handed, which is what makes a cookie or header based resolver possible at all */
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

/* nobody supported the request, so nobody authenticated: an anonymous token, no login event, and no error */
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

/* a successful authentication emits the login success event once */
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

/* an authenticator that answers a nil token without an error yields an anonymous token and NO success event: nobody logged in, so nothing announces that somebody did */
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

/* a failed authentication emits the failure event and hands the authentication error back untouched */
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

/* when the failure event dispatch ITSELF fails, the authentication error survives as the cause: it carries the 401 the client should see, which a bare dispatch error would replace with a 500 while dropping the reason from the log */
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

/* a success event whose listener fails refuses the resolution: the login already happened, and a listener that could not run is not something to serve the request past */
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

/* recordSecurityLoginEvents plants a listener on both login events and hands back the slice of names it observed, in order. */
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

/* The resolver is the application's function; a nil pointer of its own token type reaches here as a non-nil
interface and, read as a live token, is published into the security context every voter then reads. */
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
