package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func TestSessionLoginHandlerWritesTheIdentityOnTheSessionTheRequestCarries(t *testing.T) {
    managerStorage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    manager := melodysession.NewManager(managerStorage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return managerStorage, nil
    }, melodycontainer.RegisterOptions{})

    registerErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return manager, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register session manager: %v", registerErr)
    }

    registerAdmissionClock(t, containerInstance)
    registerLandingUrlGenerator(t, containerInstance)

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)

    arrivedSession := mustSession(manager.NewSession(exampleTestRuntime()))
    arrivedSession.Set("cart", "kept across the login")
    _ = melodyhttp.SetRequestSession(request, arrivedSession)

    result, loginErr := NewSessionLoginHandler(currentAccountLookup, (&recordingSessionIndex{}).lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    if nil == result || nil == result.Response || nethttp.StatusOK != result.Response.StatusCode() {
        t.Fatalf("expected the login door to answer 200, got %+v", result)
    }

    publishedSession := sessionOnRequest(request)
    if nil == publishedSession {
        t.Fatal("the request carries no session after the login")
    }

    if "user-1" != publishedSession.String(SessionKeySecurityUserId) {
        t.Fatalf("expected the identity on the rotated session, got %q", publishedSession.String(SessionKeySecurityUserId))
    }

    if "kept across the login" != publishedSession.String("cart") {
        t.Fatal("the values the client held before the login did not stay")
    }
}

func TestSessionLoginHandlerRefusesARequestWithoutASession(t *testing.T) {
    result, loginErr := NewSessionLoginHandler(currentAccountLookup, nil).Login(
        nil,
        plainRequest(t),
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
    )
    if nil != result || nil == loginErr {
        t.Fatalf("expected a refusal, so no success is announced and no token published, got %+v and %v", result, loginErr)
    }
}

func loginRequest(t *testing.T) (melodycontainercontract.Runtime, melodyhttpcontract.Request) {
    t.Helper()

    managerStorage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    manager := melodysession.NewManager(managerStorage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return managerStorage, nil
    }, melodycontainer.RegisterOptions{})

    registerErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return manager, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register session manager: %v", registerErr)
    }

    registerAdmissionClock(t, containerInstance)
    registerLandingUrlGenerator(t, containerInstance)

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)
    _ = melodyhttp.SetRequestSession(request, mustSession(manager.NewSession(exampleTestRuntime())))

    return runtimeInstance, request
}

func TestSessionLoginHandlerAnswersTheLookupsFailure(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    _, loginErr := NewSessionLoginHandler(refusingAccountLookup, nil).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"}, nil)},
    )
    if false == errors.Is(loginErr, errAccountRepositoryUnavailable) {
        t.Fatalf("expected the lookup's failure, got %v", loginErr)
    }
}

func TestSessionLoginHandlerOpensASessionThePasswordChangeRevokes(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    account := entity.NewUser("user-2", "editor", "original-hash", []string{"ROLE_EDITOR"})
    lookup := func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
        return account, true, nil
    }

    _, loginErr := NewSessionLoginHandler(lookup, (&recordingSessionIndex{}).lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_ADMIN"}, nil)},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    publishedSession := sessionOnRequest(request)
    roles, _ := getStringSliceFromSession(publishedSession, SessionKeySecurityRoles)
    if 1 != len(roles) || "ROLE_EDITOR" != roles[0] {
        t.Fatalf("expected the account's roles in the session, got %v", roles)
    }

    token := resolveToken(SessionTokenResolver(lookup), request)
    if false == token.IsAuthenticated() || "user-2" != token.UserIdentifier() {
        t.Fatalf("expected the new session to authenticate the account, got %v", token.IsAuthenticated())
    }

    account.Password = "replacement-hash"

    if true == resolveToken(SessionTokenResolver(lookup), request).IsAuthenticated() {
        t.Fatal("the session outlived the password change")
    }
}

func TestSessionLoginHandlerRefusesATokenThatIsNotAuthenticated(t *testing.T) {
    for name, token := range map[string]melodysecuritycontract.Token{
        "no token":        nil,
        "anonymous token": melodysecurity.NewAnonymousToken(),
    } {
        t.Run(name, func(t *testing.T) {
            runtimeInstance, request := loginRequest(t)

            _, loginErr := NewSessionLoginHandler(currentAccountLookup, nil).Login(runtimeInstance, request, melodysecuritycontract.LoginInput{Token: token})
            if nil == loginErr {
                t.Fatal("expected the login door to refuse a token that is not authenticated")
            }

            if true == sessionOnRequest(request).Has(SessionKeySecurityUserId) {
                t.Fatal("an identity was written for a token that is not authenticated")
            }
        })
    }
}

func TestSessionLoginHandlerRefusesAnAccountThatIsNotAvailable(t *testing.T) {
    for name, lookup := range map[string]SessionUserLookup{
        "a failing lookup": refusingAccountLookup,
        "an absent account": func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
            return nil, false, nil
        },
        "an account the lookup did not find": func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", testPasswordHash, []string{"ROLE_EDITOR"}), false, nil
        },
        "another account": func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
            return entity.NewUser("another-user", "editor", testPasswordHash, []string{"ROLE_EDITOR"}), true, nil
        },
        "an account without a password hash": func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", "", []string{"ROLE_EDITOR"}), true, nil
        },
        "an account without roles": func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", testPasswordHash, nil), true, nil
        },
    } {
        t.Run(name, func(t *testing.T) {
            runtimeInstance, request := loginRequest(t)

            _, loginErr := NewSessionLoginHandler(lookup, nil).Login(
                runtimeInstance,
                request,
                melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"}, nil)},
            )
            if nil == loginErr {
                t.Fatal("expected the login door to refuse an account that is not available")
            }

            if true == sessionOnRequest(request).Has(SessionKeySecurityUserId) {
                t.Fatal("an identity was written for an account that is not available")
            }
        })
    }
}

func TestSessionLoginHandlerAdmitsTheRotatedSessionUnderTheAccount(t *testing.T) {
    runtimeInstance, request := loginRequest(t)
    arrivedSessionId := sessionOnRequest(request).Id()

    index := &recordingSessionIndex{}

    _, loginErr := NewSessionLoginHandler(currentAccountLookup, index.lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil), PreviousSessionId: arrivedSessionId},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    expected := sessionAdmission{userId: "user-1", previousSessionId: arrivedSessionId, sessionId: sessionOnRequest(request).Id(), createdAt: sessionAdmissionTestInstant}
    if 1 != len(index.admittedList) || expected != index.admittedList[0] {
        t.Fatalf("expected one admission %+v, got %+v", expected, index.admittedList)
    }
}

func TestSessionLoginHandlerRefusedAdmissionOpensNoSession(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    admitErr := errors.New("the session index is down")
    index := &recordingSessionIndex{admitErr: admitErr}

    _, loginErr := NewSessionLoginHandler(currentAccountLookup, index.lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
    )
    if false == errors.Is(loginErr, admitErr) {
        t.Fatalf("expected the index's refusal, got %v", loginErr)
    }

    if false == sessionOnRequest(request).IsCleared() {
        t.Fatal("expected the rotated session cleared, so the response path deletes it rather than saving it")
    }
}

func registerLandingUrlGenerator(t *testing.T, containerInstance melodycontainercontract.Container) {
    t.Helper()

    melodycontainer.MustRegister[melodyhttpcontract.UrlGenerator](containerInstance, melodyhttp.ServiceUrlGenerator, func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.UrlGenerator, error) {
        return melodyhttp.NewUrlGenerator(melodyhttp.NewRouter().RouteRegistry(), melodyhttp.UrlGeneratorConfig{}), nil
    }, melodycontainer.RegisterOptions{})
}

type runtimeRecordingSessionIndex struct {
    recordingSessionIndex
    runtimes []melodycontainercontract.Runtime
}

func (instance *runtimeRecordingSessionIndex) lookupRecording(runtimeInstance melodycontainercontract.Runtime) (SessionIndex, error) {
    instance.runtimes = append(instance.runtimes, runtimeInstance)

    return &instance.recordingSessionIndex, nil
}

func TestSessionLoginHandlerReadsTheAccountAndAdmitsThroughTheRuntimeItWasHanded(t *testing.T) {
    managerStorage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    manager := melodysession.NewManager(managerStorage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return managerStorage, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister[melodysessioncontract.Manager](containerInstance, melodysession.ServiceSessionManager, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
        return manager, nil
    }, melodycontainer.RegisterOptions{})

    registerAdmissionClock(t, containerInstance)
    registerLandingUrlGenerator(t, containerInstance)

    handedRuntime := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    otherRuntime := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, otherRuntime, nil)
    _ = melodyhttp.SetRequestSession(request, mustSession(manager.NewSession(exampleTestRuntime())))

    var lookupRuntimes []melodycontainercontract.Runtime
    lookup := func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
        lookupRuntimes = append(lookupRuntimes, runtimeInstance)

        return currentAccountLookup(runtimeInstance, userId)
    }
    index := &runtimeRecordingSessionIndex{}

    _, loginErr := NewSessionLoginHandler(lookup, index.lookupRecording).Login(
        handedRuntime,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"}, nil)},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    if 1 != len(lookupRuntimes) || handedRuntime != lookupRuntimes[0] {
        t.Fatalf("expected the account read through the handed runtime, got %v", lookupRuntimes)
    }

    if 1 != len(index.runtimes) || handedRuntime != index.runtimes[0] {
        t.Fatalf("expected the session admitted through the handed runtime, got %v", index.runtimes)
    }
}
