package handler

import (
    "bytes"
    "context"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/security"
    "github.com/precision-soft/melody/v2/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysession "github.com/precision-soft/melody/v2/session"
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

/* passThroughCache holds nothing and refuses nothing, so a lookup reaches the repository every time. */
type passThroughCache struct{}

func (instance *passThroughCache) Get(key string) (any, bool, error) {
    return nil, false, nil
}

func (instance *passThroughCache) Set(key string, value any, ttl time.Duration) error {
    return nil
}

func (instance *passThroughCache) Delete(key string) error {
    return nil
}

func (instance *passThroughCache) Has(key string) (bool, error) {
    return false, nil
}

func (instance *passThroughCache) Clear() error {
    return nil
}

func (instance *passThroughCache) Many(keys []string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (instance *passThroughCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *passThroughCache) DeleteMultiple(keys []string) error {
    return nil
}

func (instance *passThroughCache) Increment(key string, delta int64) (int64, error) {
    return delta, nil
}

func (instance *passThroughCache) Decrement(key string, delta int64) (int64, error) {
    return -delta, nil
}

func (instance *passThroughCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*passThroughCache)(nil)

/* loginRuntimeOverAnEmptyDirectory wires the user service the login door resolves over a directory holding no account, so a credential that reaches the authentication is refused as invalid and one that never reaches it is refused as absent input. */
func loginRuntimeOverAnEmptyDirectory(t *testing.T) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(repository.NewInMemoryUserRepository(), &passThroughCache{}, nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

/* the credentials are read from the body alone: a POST whose query string carries them and whose form body is empty answers as a request without credentials, where FormValue would have read the query and authenticated — with the credentials written into every access log in front of the application. The body form of the same credentials reaches the authentication, which the empty directory refuses, so the two arms are told apart by the status. */
func TestLoginHandler_ReadsTheFormCredentialsFromTheBodyNotTheQuery(t *testing.T) {
    runtimeInstance := loginRuntimeOverAnEmptyDirectory(t)

    login := func(target string, body string) (int, string) {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, target, bytes.NewBufferString(body))
        httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-form-test", time.Now()))

        response, handlerErr := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr {
            t.Fatalf("login handler: %v", handlerErr)
        }

        bodyBytes, readErr := io.ReadAll(response.BodyReader())
        if nil != readErr {
            t.Fatalf("read response body: %v", readErr)
        }

        return response.StatusCode(), string(bodyBytes)
    }

    statusCode, body := login("/login?username=admin&password=secret", "")
    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "invalid credentials input") {
        t.Fatalf("credentials carried by the query were read: status %d, body %s", statusCode, body)
    }

    statusCode, body = login("/login", "username=admin&password=secret")
    if nethttp.StatusUnauthorized != statusCode || false == strings.Contains(body, "invalid credentials") {
        t.Fatalf("credentials carried by the body did not reach the authentication: status %d, body %s", statusCode, body)
    }
}
