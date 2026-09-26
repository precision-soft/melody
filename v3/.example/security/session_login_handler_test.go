package security

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
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

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)

    arrivedSession := manager.NewSession()
    arrivedSession.Set("cart", "kept across the login")
    request.Attributes().Set(melodyhttp.RequestAttributeSession, arrivedSession)

    result, loginErr := NewSessionLoginHandler().Login(
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
    result, loginErr := NewSessionLoginHandler().Login(
        nil,
        plainRequest(t),
        melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})},
    )
    if nil != loginErr {
        t.Fatalf("the login door failed: %v", loginErr)
    }

    if nil == result || nil == result.Response || nethttp.StatusInternalServerError != result.Response.StatusCode() {
        t.Fatalf("expected the login door to answer 500 without a session, got %+v", result)
    }
}
