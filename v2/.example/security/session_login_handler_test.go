package security

import (
    "context"
    "testing"
    "time"
    "net/http/httptest"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
    melodysession "github.com/precision-soft/melody/v2/session"
    melodysessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

func TestSessionLoginHandlerCreatesRevocableSession(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()
    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    melodycontainer.MustRegister[melodysessioncontract.Manager](serviceContainer, melodysession.ServiceSessionManager, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
        return manager, nil
    })
    runtimeInstance := melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    request := melodyhttp.NewRequest(httptest.NewRequest("POST", "/login/", nil), nil, runtimeInstance, nil)
    originalSession := manager.NewSession()
    originalId := originalSession.Id()
    request.Attributes().Set(melodyhttp.RequestAttributeSession, originalSession)
    user := entity.NewUser("user-2", "editor", "original-hash", []string{"ROLE_EDITOR"})
    lookup := func(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
        return user, true, nil
    }
    result, err := NewSessionLoginHandler(lookup).Login(runtimeInstance, request, melodysecuritycontract.LoginInput{Token: melodysecurity.NewAuthenticatedToken(user.Id, user.Roles)})
    if nil != err || nil == result {
        t.Fatalf("login failed: %v", err)
    }
    if originalId == getSession(request).Id() {
        t.Fatal("login did not rotate the session")
    }
    resolver := SessionTokenResolver(lookup)
    if false == resolver(request).IsAuthenticated() {
        t.Fatal("new session was not authenticated")
    }
    user.Password = "new-hash"
    if true == resolver(request).IsAuthenticated() {
        t.Fatal("password change did not invalidate the session")
    }
}
