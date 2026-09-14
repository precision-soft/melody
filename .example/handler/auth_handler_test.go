package handler

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"
    "github.com/precision-soft/melody/.example/security"
    melodyhttp "github.com/precision-soft/melody/http"
    melodysession "github.com/precision-soft/melody/session"
    "context"
    "strings"
    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/service"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/runtime"
)

/* The logout door is asked what the STORAGE holds afterwards, not what the session object says: deleting the two identity keys leaves the entry modified, so the response path saves it back under the same id and re-issues the cookie, and only a cleared session routes that path to DeleteSession. The response path is run here exactly as the kernel runs it, through SaveSession. */
func TestLogoutHandlerEndsTheSessionRatherThanEmptyingIt(t *testing.T) {
    storage := melodysession.NewInMemoryStorage()
    defer storage.Close()

    manager := melodysession.NewManager(storage, time.Hour)

    sessionInstance := manager.NewSession()
    sessionInstance.Set(security.SessionKeySecurityUserId, "user-1")
    sessionInstance.Set(security.SessionKeySecurityRoles, []string{"ROLE_USER"})

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    if _, exists, _ := storage.Load(sessionId); false == exists {
        t.Fatal("the storage did not hold the session the test is about to end")
    }

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/logout/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, nil, nil)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    response, err := LogoutHandler()(nil, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == response {
        t.Fatal("the logout door answered no response")
    }

    if false == sessionInstance.IsCleared() {
        t.Fatal("the session was not marked cleared, so the response path will save it back under the same id")
    }

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the response path refused the cleared session: %v", err)
    }

    data, exists, loadErr := storage.Load(sessionId)
    if nil != loadErr {
        t.Fatalf("the storage refused the read: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("the session entry outlived the logout that ended it, holding %v", data)
    }
}

/* a logout that arrives without a session is not an error: the door answers the same redirect, and nothing is left behind to end */
func TestLogoutHandlerToleratesARequestCarryingNoSession(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/logout/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, nil, nil)

    response, err := LogoutHandler()(nil, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == response {
        t.Fatal("the logout door answered no response")
    }
}


type loginBodyRepository struct {
    repository.UserRepository
    usernames []string
}

func (instance *loginBodyRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    instance.usernames = append(instance.usernames, username)
    return nil, false, nil
}

/* Query credentials must never reach authentication, including when they fill a missing body field. */
func TestLoginHandlerRequiresCredentialsInBody(t *testing.T) {
    for _, testCase := range []struct {
        name, query, body, contentType string
        status, lookups int
    }{
        {"query only", "username=url-user&password=url-secret", "", "application/x-www-form-urlencoded", 400, 0},
        {"password from query", "password=url-secret", "username=body-user", "application/x-www-form-urlencoded", 400, 0},
        {"username from query", "username=url-user", "password=body-secret", "application/x-www-form-urlencoded", 400, 0},
        {"form body", "username=url-user&password=url-secret", "username=body-user&password=body-secret", "application/x-www-form-urlencoded", 401, 1},
        {"json body", "username=url-user&password=url-secret", `{"username":"body-user","password":"body-secret"}`, "application/json", 401, 1},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            repositoryInstance := &loginBodyRepository{}
            containerInstance := container.NewContainer()
            if err := container.Register[*service.UserService](containerInstance, service.ServiceUserService,
                func(resolver containercontract.Resolver) (*service.UserService, error) {
                    return service.NewUserService(repositoryInstance, nil, nil), nil
                }); nil != err {
                t.Fatal(err)
            }
            runtimeInstance := runtime.New(context.Background(), containerInstance.NewScope(), containerInstance)
            httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login?"+testCase.query, strings.NewReader(testCase.body))
            httpRequest.Header.Set("Content-Type", testCase.contentType)
            request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)
            response, err := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
            if nil != err || nil == response {
                t.Fatalf("handler response=%v error=%v", response, err)
            }
            if testCase.status != response.StatusCode() || testCase.lookups != len(repositoryInstance.usernames) {
                t.Fatalf("status=%d authentication lookups=%v; want status=%d lookups=%d", response.StatusCode(), repositoryInstance.usernames, testCase.status, testCase.lookups)
            }
            if 1 == testCase.lookups && "body-user" != repositoryInstance.usernames[0] {
                t.Fatalf("authenticated username from wrong source: %v", repositoryInstance.usernames)
            }
        })
    }
}
