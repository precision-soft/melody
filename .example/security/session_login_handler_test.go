package security

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
    melodysession "github.com/precision-soft/melody/session"
    melodysessioncontract "github.com/precision-soft/melody/session/contract"
)

/* the session a client held before authenticating is the one an attacker could have planted, so the identity must land on a session under a fresh id and never on the one the request arrived with */
func TestSessionLoginHandlerWritesTheIdentityUnderARotatedId(t *testing.T) {
    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return manager, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register session manager: %v", registerErr)
    }

    registerAdmissionClock(t, containerInstance)

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)

    arrivedSession := manager.NewSession()
    arrivedSession.Set("cart", "kept across the login")
    request.Attributes().Set(melodyhttp.RequestAttributeSession, arrivedSession)

    result, loginErr := NewSessionLoginHandler(currentAccountLookup, (&recordingSessionIndex{}).lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    if nil == result || nil == result.Response || nethttp.StatusOK != result.Response.StatusCode() {
        t.Fatalf("expected the login door to answer 200, got %+v", result)
    }

    publishedSession := getSession(request)
    if nil == publishedSession {
        t.Fatal("the request carries no session after the login")
    }

    if arrivedSession.Id() == publishedSession.Id() {
        t.Fatal("the identity was written under the id the request arrived with")
    }

    if "user-1" != publishedSession.String(SessionKeySecurityUserId) {
        t.Fatalf("expected the identity on the rotated session, got %q", publishedSession.String(SessionKeySecurityUserId))
    }

    if true == arrivedSession.Has(SessionKeySecurityUserId) {
        t.Fatal("the session the request arrived with carries the identity")
    }

    if "kept across the login" != publishedSession.String("cart") {
        t.Fatal("the values the client held before the login did not carry over to the rotated session")
    }
}

func TestSessionLoginHandlerAnswersAServerErrorWithoutASession(t *testing.T) {
    result, loginErr := NewSessionLoginHandler(currentAccountLookup, nil).Login(
        nil,
        requestAccepting(t, ""),
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    if nil == result || nil == result.Response || nethttp.StatusInternalServerError != result.Response.StatusCode() {
        t.Fatalf("expected the login door to answer 500 without a session, got %+v", result)
    }
}

/* loginRequest builds a login request carrying a fresh session, over a runtime whose container holds the session manager the rotation needs */
func loginRequest(t *testing.T) (melodyruntimecontract.Runtime, melodyhttpcontract.Request) {
    t.Helper()

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return manager, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register session manager: %v", registerErr)
    }

    registerAdmissionClock(t, containerInstance)

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, manager.NewSession())

    return runtimeInstance, request
}

/* the lookup's own failure is what the login door answers, not a refusal of the account: the repository being down says nothing about the account */
func TestSessionLoginHandlerAnswersTheLookupsFailure(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    _, loginErr := NewSessionLoginHandler(refusingAccountLookup, nil).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"})},
    )
    if false == errors.Is(loginErr, errAccountRepositoryUnavailable) {
        t.Fatalf("expected the lookup's failure, got %v", loginErr)
    }
}

/* the session is written from the account, not from the token the authenticator built: the roles are the account's, and the credential version ties the session to the password hash, so a password change revokes it on the next request */
func TestSessionLoginHandlerOpensASessionThePasswordChangeRevokes(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    account := entity.NewUser("user-2", "editor", "original-hash", []string{"ROLE_EDITOR"})
    lookup := func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
        return account, true, nil
    }

    _, loginErr := NewSessionLoginHandler(lookup, (&recordingSessionIndex{}).lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_ADMIN"})},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    publishedSession := getSession(request)
    roles, _ := getStringSliceFromSession(publishedSession, SessionKeySecurityRoles)
    if 1 != len(roles) || "ROLE_EDITOR" != roles[0] {
        t.Fatalf("expected the account's roles in the session, got %v", roles)
    }

    token := SessionTokenResolver(lookup)(request)
    if false == token.IsAuthenticated() || "user-2" != token.UserIdentifier() {
        t.Fatalf("expected the new session to authenticate the account, got %v", token.IsAuthenticated())
    }

    account.Password = "replacement-hash"

    if true == SessionTokenResolver(lookup)(request).IsAuthenticated() {
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

            if true == getSession(request).Has(SessionKeySecurityUserId) {
                t.Fatal("an identity was written for a token that is not authenticated")
            }
        })
    }
}

func TestSessionLoginHandlerRefusesAnAccountThatIsNotAvailable(t *testing.T) {
    for name, lookup := range map[string]SessionUserLookup{
        "a failing lookup": refusingAccountLookup,
        "an absent account": func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
            return nil, false, nil
        },
        "an account the lookup did not find": func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", testPasswordHash, []string{"ROLE_EDITOR"}), false, nil
        },
        "another account": func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
            return entity.NewUser("another-user", "editor", testPasswordHash, []string{"ROLE_EDITOR"}), true, nil
        },
        "an account without a password hash": func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", "", []string{"ROLE_EDITOR"}), true, nil
        },
        "an account without roles": func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
            return entity.NewUser(userId, "editor", testPasswordHash, nil), true, nil
        },
    } {
        t.Run(name, func(t *testing.T) {
            runtimeInstance, request := loginRequest(t)

            _, loginErr := NewSessionLoginHandler(lookup, nil).Login(
                runtimeInstance,
                request,
                melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"})},
            )
            if nil == loginErr {
                t.Fatal("expected the login door to refuse an account that is not available")
            }

            if true == getSession(request).Has(SessionKeySecurityUserId) {
                t.Fatal("an identity was written for an account that is not available")
            }
        })
    }
}

/* the firewall's sign-in admits the rotated session under the account, naming the id the rotation retired, so a session opened through the firewall counts against the account's cap as one opened at the sign-in door does */
func TestSessionLoginHandlerAdmitsTheRotatedSessionUnderTheAccount(t *testing.T) {
    runtimeInstance, request := loginRequest(t)
    arrivedSessionId := getSession(request).Id()

    index := &recordingSessionIndex{}

    _, loginErr := NewSessionLoginHandler(currentAccountLookup, index.lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    expected := sessionAdmission{userId: "user-1", previousSessionId: arrivedSessionId, sessionId: getSession(request).Id(), createdAt: sessionAdmissionTestInstant}
    if 1 != len(index.admittedList) || expected != index.admittedList[0] {
        t.Fatalf("expected one admission %+v, got %+v", expected, index.admittedList)
    }
}

/* an admission the index refuses fails the sign-in and clears the rotated session, so the response path stores nothing the cap does not count */
func TestSessionLoginHandlerRefusedAdmissionOpensNoSession(t *testing.T) {
    runtimeInstance, request := loginRequest(t)

    admitErr := errors.New("the session index is down")
    index := &recordingSessionIndex{admitErr: admitErr}

    _, loginErr := NewSessionLoginHandler(currentAccountLookup, index.lookup).Login(
        runtimeInstance,
        request,
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
    )
    if false == errors.Is(loginErr, admitErr) {
        t.Fatalf("expected the index's refusal, got %v", loginErr)
    }

    if false == getSession(request).IsCleared() {
        t.Fatal("expected the rotated session cleared, so the response path deletes it rather than saving it")
    }
}
