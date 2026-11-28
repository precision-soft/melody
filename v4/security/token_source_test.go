package security

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func newTokenSourceTestRuntime(t *testing.T) (containercontract.Runtime, eventcontract.EventDispatcher) {
    t.Helper()

    return newTokenSourceTestRuntimeLogging(t, logging.NewNopLogger())
}

func newTokenSourceTestRuntimeLogging(t *testing.T, logger loggingcontract.Logger) (containercontract.Runtime, eventcontract.EventDispatcher) {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    dispatcher := event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()})

    registerErr := serviceContainer.Register(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    loggerRegisterErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        container.RegisterOptions{},
    )
    if nil != loggerRegisterErr {
        t.Fatalf("unexpected error: %v", loggerRegisterErr)
    }

    return runtime.NewRequest(context.Background(), serviceContainer.NewScope()), dispatcher
}

func TestNewResolverTokenSource_NilResolverPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewResolverTokenSource(nil, ResolverTokenSourceConfig{})
        },
        "token resolver is nil",
    )
}

func TestResolverTokenSource_NameIdentifiesTheSource(t *testing.T) {
    tokenSource := NewResolverTokenSource(
        func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return nil, nil
        },
        ResolverTokenSourceConfig{},
    )

    if "tokenResolver" != tokenSource.Name() {
        t.Fatalf("expected the source to name itself tokenResolver, got %q", tokenSource.Name())
    }
}

func TestResolverTokenSource_ResolveAnswersAnonymousWhenTheResolverFindsNobody(t *testing.T) {
    runtimeInstance, _ := newTokenSourceTestRuntime(t)

    tokenSource := NewResolverTokenSource(
        func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return nil, nil
        },
        ResolverTokenSourceConfig{},
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

    resolvedToken := NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)

    tokenSource := NewResolverTokenSource(
        func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return resolvedToken, nil
        },
        ResolverTokenSourceConfig{},
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
        func(runtimeInstance containercontract.Runtime, resolverRequest httpcontract.Request) (securitycontract.Token, error) {
            seenRequest = resolverRequest

            return nil, nil
        },
        ResolverTokenSourceConfig{},
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
            _ = NewAuthenticatorTokenSource(nil, AuthenticatorTokenSourceConfig{})
        },
        "authenticator manager is nil",
    )
}

func TestAuthenticatorTokenSource_NameIdentifiesTheSource(t *testing.T) {
    tokenSource := NewAuthenticatorTokenSource(NewAuthenticatorManager(nil), AuthenticatorTokenSourceConfig{})

    if "authenticatorManager" != tokenSource.Name() {
        t.Fatalf("expected the source to name itself authenticatorManager, got %q", tokenSource.Name())
    }
}

func TestAuthenticatorTokenSource_ResolveAnswersAnonymousWhenNoAuthenticatorSupports(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return false },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    t.Fatalf("should not be called")

                    return nil, nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
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
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil), nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
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

func TestAuthenticatorTokenSource_ResolveAnnouncesRejectedCredentialsAsAFailure(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected an anonymous token")
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected exactly one login failure event for rejected credentials, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveEmitsLoginFailureAndKeepsTheError(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    authenticationErr := exception.Unauthorized("invalid credentials")

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, authenticationErr
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
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
    logger := &recordingLogger{}
    runtimeInstance, dispatcher := newTokenSourceTestRuntimeLogging(t, logger)

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("event bus down")
        },
        eventcontract.ListenerConfig{},
    )

    authenticationErr := exception.Unauthorized("invalid credentials")

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, authenticationErr
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    _, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if authenticationErr != err {
        t.Fatalf("expected the authentication error handed back as it is, got %v", err)
    }

    records := logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(records) || "security login failure event dispatch failed" != records[0].message || false == strings.Contains(fmt.Sprintf("%v", records[0].context), "event bus down") || "authenticatorManager" != records[0].context["tokenSourceName"] {
        t.Fatalf("expected one Error record of the failed dispatch naming the source and the listener's failure, got %v", logger.records)
    }
}

func TestAuthenticatorTokenSource_ResolveFailsWhenTheSuccessDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchErr := errors.New("event bus down")

    dispatcher.AddListener(
        securitycontract.EventSecurityLoginSuccess,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return dispatchErr
        },
        eventcontract.ListenerConfig{},
    )

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil), nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil == err {
        t.Fatalf("expected a failed success dispatch to refuse the resolution")
    }

    if nil != token {
        t.Fatalf("expected no token alongside the refusal, got %v", token)
    }
}

func TestAuthenticatorTokenSource_APendingSecondFactorIsNotAnnouncedAsAFailure(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewTwoFactorPendingToken(NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)), nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if pendingUser, isPending := PendingUserFromToken(token); false == isPending || "user-1" != pendingUser {
        t.Fatalf("expected the pending challenge to reach the application, got present=%v user=%q", isPending, pendingUser)
    }

    if 0 != len(*dispatchedNameList) {
        t.Fatalf("expected no login event for a pending second factor, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ARejectedSecondFactorIsAnnouncedAsAFailure(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    failureMessageList := make([]string, 0)
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            failureEvent, isFailureEvent := eventValue.Payload().(*LoginFailureEvent)
            if false == isFailureEvent {
                t.Fatalf("expected a login failure event, got %T", eventValue.Payload())
            }

            failureMessageList = append(failureMessageList, failureEvent.Error().Error())

            return nil
        },
        eventcontract.ListenerConfig{},
    )

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewTwoFactorRejectedToken(NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)), nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if _, isPending := PendingUserFromToken(token); false == isPending {
        t.Fatalf("expected the rejected challenge to stay pending so the application prompts again")
    }

    if 1 != len(failureMessageList) || "security second factor rejected" != failureMessageList[0] {
        t.Fatalf("expected exactly one failure announcing the rejected second factor, got %v", failureMessageList)
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
            func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
                dispatchedNameList = append(dispatchedNameList, recordedName)

                return nil
            },
            eventcontract.ListenerConfig{},
        )
    }

    return &dispatchedNameList
}

func TestResolverTokenSource_ResolveReadsATypedNilTokenAsAbsent(t *testing.T) {
    tokenSource := NewResolverTokenSource(
        func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            var unassignedToken *AuthenticatedToken

            return unassignedToken, nil
        },
        ResolverTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(nil, nil)

    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }

    if _, isAnonymous := token.(*AnonymousToken); false == isAnonymous {
        t.Fatalf("expected the anonymous token, got %T", token)
    }
}

func TestAuthenticatorTokenSource_AWrongApiKeyIsAnnouncedAsALoginFailure(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            NewApiKeyHeaderAuthenticator("right", ApiKeyHeaderAuthenticatorConfig{
                HeaderName:     "X-Api-Key",
                UserIdentifier: "service",
                Roles:          []string{"ROLE_SERVICE"},
            }),
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(
        runtimeInstance,
        newSecurityTestRequest("GET", "/", map[string]string{"X-Api-Key": "wrong"}, runtimeInstance),
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected the wrong key answered anonymous")
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected exactly one login failure event, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_TheFirstSupportingAuthenticatorDecides(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            NewApiKeyHeaderAuthenticator("right", ApiKeyHeaderAuthenticatorConfig{
                HeaderName:     "X-Api-Key",
                UserIdentifier: "service",
                Roles:          []string{"ROLE_SERVICE"},
            }),
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    t.Fatalf("the second authenticator ran after the first supported the request")

                    return nil, nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    token, err := tokenSource.Resolve(
        runtimeInstance,
        newSecurityTestRequest("GET", "/", map[string]string{"X-Api-Key": "wrong"}, runtimeInstance),
    )
    if nil != err || true == token.IsAuthenticated() {
        t.Fatalf("expected an anonymous answer, got token=%v err=%v", token, err)
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected exactly one login failure event, got %v", *dispatchedNameList)
    }
}

func TestAuthenticatorTokenSource_ResolveFailsWhenTheRejectedCredentialsDispatchFails(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    listenerErr := errors.New("event bus down")
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return listenerErr
        },
        eventcontract.ListenerConfig{},
    )

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            NewApiKeyHeaderAuthenticator("right", ApiKeyHeaderAuthenticatorConfig{
                HeaderName:     "X-Api-Key",
                UserIdentifier: "service",
                Roles:          []string{"ROLE_SERVICE"},
            }),
        }),
        AuthenticatorTokenSourceConfig{},
    )

    _, err := tokenSource.Resolve(
        runtimeInstance,
        newSecurityTestRequest("GET", "/", map[string]string{"X-Api-Key": "wrong"}, runtimeInstance),
    )
    if false == errors.Is(err, listenerErr) {
        t.Fatalf("expected the listener's failure to fail the resolution, got %v", err)
    }
}

func TestAuthenticatorTokenSource_AFailedLoginFailureDispatchKeepsTheStatusAndFilesTheListenersError(t *testing.T) {
    logger := &recordingLogger{}
    runtimeInstance, dispatcher := newTokenSourceTestRuntimeLogging(t, logger)

    listenerErr := exception.NewHttpException(503, "event bus down")
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return listenerErr
        },
        eventcontract.ListenerConfig{},
    )

    authenticationErr := exception.NewHttpException(401, "invalid credentials")

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return nil, authenticationErr
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )

    _, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if false == errors.Is(err, authenticationErr) || true == errors.Is(err, listenerErr) {
        t.Fatalf("expected the authentication error alone, a listener's 503 kept out of the answer, got %v", err)
    }

    if 1 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatalf("expected the listener's failure filed at Error, got %v", logger.records)
    }
}

func TestAuthenticatorTokenSource_AFailedRejectedCredentialsDispatchKeepsTheRejection(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)

    var announcedErr error
    listenerErr := errors.New("event bus down")
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            if failureEvent, ok := eventValue.Payload().(*LoginFailureEvent); true == ok {
                announcedErr = failureEvent.Error()
            }

            return listenerErr
        },
        eventcontract.ListenerConfig{},
    )

    tokenSource := NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            NewApiKeyHeaderAuthenticator("right", ApiKeyHeaderAuthenticatorConfig{
                HeaderName:     "X-Api-Key",
                UserIdentifier: "service",
                Roles:          []string{"ROLE_SERVICE"},
            }),
        }),
        AuthenticatorTokenSourceConfig{},
    )

    _, err := tokenSource.Resolve(
        runtimeInstance,
        newSecurityTestRequest("GET", "/", map[string]string{"X-Api-Key": "wrong"}, runtimeInstance),
    )
    if nil == announcedErr || false == errors.Is(err, announcedErr) || false == errors.Is(err, listenerErr) {
        t.Fatalf("expected the rejection kept beside the dispatch error, got %v", err)
    }
}

func authenticatorTokenSourceAnswering(token securitycontract.Token, err error) *AuthenticatorTokenSource {
    return NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return token, err
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )
}

func TestAuthenticatorTokenSource_AnAuthenticatorThatCouldNotCheckTheCredentialIsNoLoginFailure(t *testing.T) {
    storeErr := errors.New("redis is down")

    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)
    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    _, err := authenticatorTokenSourceAnswering(nil, exception.MarkInfrastructureFailure(storeErr)).Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if false == errors.Is(err, storeErr) || false == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the marked error handed back, got %v", err)
    }

    unavailable := NewUnavailableToken(exception.MarkInfrastructureFailure(storeErr))
    token, tokenErr := authenticatorTokenSourceAnswering(unavailable, nil).Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != tokenErr || securitycontract.Token(unavailable) != token {
        t.Fatalf("expected the unavailable token passed through, got %T, %v", token, tokenErr)
    }

    if 0 != len(*dispatchedNameList) {
        t.Fatalf("expected no sign-in event for a credential nobody could check, got %v", *dispatchedNameList)
    }

    statusErr := exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "too many", Cause: exception.MarkInfrastructureFailure(storeErr)})
    if _, keptErr := authenticatorTokenSourceAnswering(nil, statusErr).Resolve(runtimeInstance, newFirewallTestRequest("/")); nil == keptErr {
        t.Fatal("expected the error that keeps its status handed back")
    }
    if 0 != len(*dispatchedNameList) {
        t.Fatalf("expected no failure event for a 429 kept around the mark, a spent budget being no refused credential, got %v", *dispatchedNameList)
    }

    refusedErr := exception.NewHttpError(nethttp.StatusUnauthorized, exception.HttpErrorConfig{Message: "refused", Cause: exception.MarkInfrastructureFailure(storeErr)})
    if _, keptErr := authenticatorTokenSourceAnswering(nil, refusedErr).Resolve(runtimeInstance, newFirewallTestRequest("/")); nil == keptErr {
        t.Fatal("expected the error that keeps its status handed back")
    }
    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected the failure event for a 401 kept around the mark, got %v", *dispatchedNameList)
    }

    both := &unavailableAuthenticatedToken{}
    if token, bothErr := authenticatorTokenSourceAnswering(both, nil).Resolve(runtimeInstance, newFirewallTestRequest("/")); nil != bothErr || securitycontract.Token(both) != token || 1 != len(*dispatchedNameList) {
        t.Fatalf("expected a token both unavailable and authenticated passed through with no event, got %T %v %v", token, bothErr, *dispatchedNameList)
    }
}

type unavailableAuthenticatedToken struct {
    AuthenticatedToken
}

func (instance *unavailableAuthenticatedToken) IsAuthenticated() bool { return true }

func (instance *unavailableAuthenticatedToken) AuthenticationUnavailableCause() error {
    return exception.MarkInfrastructureFailure(errors.New("redis is down"))
}

func TestKernel_AnAuthenticatorOutageServesAPublicRouteAndRefusesAProtectedOneWith503(t *testing.T) {
    for name, tokenSource := range map[string]func() securitycontract.TokenSource{
        "a marked error": func() securitycontract.TokenSource {
            return authenticatorTokenSourceAnswering(nil, exception.MarkInfrastructureFailure(errors.New("redis is down")))
        },
        "an unavailable token": func() securitycontract.TokenSource {
            return authenticatorTokenSourceAnswering(NewUnavailableToken(exception.MarkInfrastructureFailure(errors.New("redis is down"))), nil)
        },
    } {
        t.Run(name, func(t *testing.T) {
            public, handled, _ := newUnavailableRouteFixtureOver(tokenSource(), bound.Of(7*time.Second)).serve(t, "/public")
            if nethttp.StatusOK != public.Code || false == handled {
                t.Fatalf("expected the public route served, got %d: %s", public.Code, public.Body.String())
            }

            protected, protectedHandled, _ := newUnavailableRouteFixtureOver(tokenSource(), bound.Of(7*time.Second)).serve(t, "/admin")
            if nethttp.StatusServiceUnavailable != protected.Code || "7" != protected.Header().Get("Retry-After") || true == protectedHandled {
                t.Fatalf("expected 503 with Retry-After 7, got %d %q: %s", protected.Code, protected.Header().Get("Retry-After"), protected.Body.String())
            }
        })
    }

    for _, path := range []string{"/public", "/admin"} {
        statusErr := exception.NewHttpError(nethttp.StatusTooManyRequests, exception.HttpErrorConfig{Message: "too many", Cause: exception.MarkInfrastructureFailure(errors.New("redis is down"))})
        recorder, _, _ := newUnavailableRouteFixtureOver(authenticatorTokenSourceAnswering(nil, statusErr), bound.Of(7*time.Second)).serve(t, path)
        if nethttp.StatusTooManyRequests != recorder.Code || "" != recorder.Header().Get("Retry-After") {
            t.Fatalf("expected a marked error that answers 429 to keep it on %s, got %d %q", path, recorder.Code, recorder.Header().Get("Retry-After"))
        }
    }
}

func TestResolverTokenSource_IsStatefulUnlessItsConfigDeclaresOtherwise(t *testing.T) {
    resolver := func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
        return nil, nil
    }

    if true == NewResolverTokenSource(resolver, ResolverTokenSourceConfig{}).Stateless() {
        t.Fatalf("expected the zero config to declare the resolver source stateful")
    }

    if false == NewResolverTokenSource(resolver, ResolverTokenSourceConfig{Stateless: true}).Stateless() {
        t.Fatalf("expected the declared resolver source to answer stateless")
    }
}

func TestAuthenticatorTokenSource_IsStatefulUnlessItsConfigDeclaresOtherwise(t *testing.T) {
    if true == NewAuthenticatorTokenSource(NewAuthenticatorManager(nil), AuthenticatorTokenSourceConfig{}).Stateless() {
        t.Fatalf("expected the zero config to declare the authenticator source stateful")
    }

    if false == NewAuthenticatorTokenSource(NewAuthenticatorManager(nil), AuthenticatorTokenSourceConfig{Stateless: true}).Stateless() {
        t.Fatalf("expected the declared authenticator source to answer stateless")
    }
}

func lockedTokenSource() *AuthenticatorTokenSource {
    return NewAuthenticatorTokenSource(
        NewAuthenticatorManager([]securitycontract.Authenticator{
            &testAuthenticator{
                supportsCallback: func(request httpcontract.Request) bool { return true },
                authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                    return NewTwoFactorLockedToken(NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil), 9*time.Second), nil
                },
            },
        }),
        AuthenticatorTokenSourceConfig{},
    )
}

func TestAuthenticatorTokenSource_LockedSecondFactor_Answers429OnEveryRoute(t *testing.T) {
    runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)
    dispatchedNameList := recordSecurityLoginEvents(dispatcher)

    failureErrors := []error{}
    dispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            if failureEvent, isFailureEvent := eventValue.Payload().(*LoginFailureEvent); true == isFailureEvent {
                failureErrors = append(failureErrors, failureEvent.Error())
            }

            return nil
        },
        eventcontract.ListenerConfig{},
    )

    token, err := lockedTokenSource().Resolve(runtimeInstance, newFirewallTestRequest("/"))
    if nil != token || false == errors.Is(err, ErrSecondFactorLocked) {
        t.Fatalf("expected the locked refusal and no token, got %v, %v", token, err)
    }

    refusal := exception.AsHttpException(err)
    if nil == refusal || nethttp.StatusTooManyRequests != refusal.StatusCode() || 9*time.Second != refusal.RetryAfter() || "authenticatorManager" != exception.LogContext(err, exception.LogContextOptions{})["tokenSourceName"] {
        t.Fatalf("expected 429 with the lockout's remainder naming the source, got %v", err)
    }

    if 1 != len(*dispatchedNameList) || securitycontract.EventSecurityLoginFailure != (*dispatchedNameList)[0] {
        t.Fatalf("expected one failure event before the refusal, got %v", *dispatchedNameList)
    }

    if 1 != len(failureErrors) || false == errors.Is(failureErrors[0], ErrSecondFactorLocked) {
        t.Fatalf("expected the failure event carrying the lockout, got %v", failureErrors)
    }

    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.TokenSource = lockedTokenSource()
    })
    fixture.kernel.httpRouter.Handle("/public", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(nethttp.StatusOK, "public"), nil
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}})

    recorder := fixture.serve(t, nethttp.MethodGet, "/public", nil)
    if nethttp.StatusTooManyRequests != recorder.Code || "9" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected a public route refused 429 with Retry-After 9, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}
