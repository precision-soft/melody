package handler

import (
    "bytes"
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/security"
    "github.com/precision-soft/melody/.example/service"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyclock "github.com/precision-soft/melody/clock"
    melodyevent "github.com/precision-soft/melody/event"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    melodyloggingcontract "github.com/precision-soft/melody/logging/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
    melodysession "github.com/precision-soft/melody/session"
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

    runtimeInstance, _ := loginRuntimeRefusingEveryCredential(t)

    return runtimeInstance
}

/* loginRuntimeRefusingEveryCredential is the runtime over the empty directory with the dispatcher a refused login reaches, and the failures that dispatcher recorded. */
func loginRuntimeRefusingEveryCredential(t *testing.T) (melodyruntimecontract.Runtime, *[]error) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, nil)

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

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), failureList
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

/* registerLoginFailureRecorder registers an event dispatcher that records the failure each security.login.failure event carries, the one listener the login door's refusal must reach; the listener answers listenerErr, so a non-nil one fails the dispatch. */
func registerLoginFailureRecorder(t *testing.T, containerInstance melodycontainercontract.Container, listenerErr error) *[]error {
    t.Helper()

    failureList := make([]error, 0, 1)

    dispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
    dispatcher.AddListener(
        melodysecuritycontract.EventSecurityLoginFailure,
        func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
            if failure, isFailure := eventValue.Payload().(*melodysecurity.LoginFailureEvent); true == isFailure {
                failureList = append(failureList, failure.Error())
            }

            return listenerErr
        },
        0,
    )

    registerErr := melodycontainer.Register[melodyeventcontract.EventDispatcher](
        containerInstance,
        melodyevent.ServiceEventDispatcher,
        func(resolver melodycontainercontract.Resolver) (melodyeventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register event dispatcher: %v", registerErr)
    }

    /* the dispatcher journals each dispatch through the runtime's logger */
    registerLoggerErr := melodycontainer.Register[melodyloggingcontract.Logger](
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )
    if nil != registerLoggerErr {
        t.Fatalf("register logger: %v", registerLoggerErr)
    }

    return &failureList
}

/* a refused login answers 401 and raises security.login.failure once, carrying a failure that names neither credential: the door authenticates the credentials itself, so the event is its own to dispatch */
func TestLoginHandler_RaisesTheLoginFailureOnRefusedCredentials(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username=nobody&password=wrong"))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-failure-test", time.Now()))

    response, handlerErr := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the refused login answered 401, got %d", response.StatusCode())
    }

    if 1 != len(*failureList) {
        t.Fatalf("expected one security.login.failure event, got %d", len(*failureList))
    }

    if false == errors.Is((*failureList)[0], errInvalidCredentials) {
        t.Fatalf("expected the event to carry the invalid credentials failure, got %v", (*failureList)[0])
    }

    if true == strings.Contains((*failureList)[0].Error(), "wrong") || true == strings.Contains((*failureList)[0].Error(), "nobody") {
        t.Fatalf("expected the failure to name neither credential, got %q", (*failureList)[0].Error())
    }
}

/* a login failure the dispatch cannot deliver still answers the refusal: the door journals the dispatch failure as the cause and keeps the 401, as the framework's token source does, rather than turning a refused password into a 500 */
func TestLoginHandler_KeepsTheRefusalWhenTheLoginFailureDispatchFails(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, errors.New("login failure listener refused"))

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(&acceptingAuthenticationRepository{}, &passThroughCache{}, nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    response := postLoginCredentials(t, runtimeInstance, "nobody", "wrong")

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the refused login answered 401 when its failure could not be dispatched, got %d", response.StatusCode())
    }

    if 1 != len(*failureList) {
        t.Fatalf("expected the login failure dispatched once, got %d", len(*failureList))
    }
}

/* accepted credentials raise no login failure: the request carries no session, so the door stops after the authentication with its 500, and the only event it could have raised by then is the one this test refuses */
func TestLoginHandler_RaisesNoLoginFailureOnAcceptedCredentials(t *testing.T) {
    passwordHash, hashErr := security.HashPassword("secret")
    if nil != hashErr {
        t.Fatalf("hash password: %v", hashErr)
    }

    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, nil)

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(
                &acceptingAuthenticationRepository{
                    user: &entity.User{Id: "user-accepted", Username: "admin", Password: passwordHash, Roles: []string{"ROLE_USER"}},
                },
                &passThroughCache{},
                nil,
            ), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    response := postLoginCredentials(t, runtimeInstance, "admin", "secret")

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the accepted login to stop at the missing session with 500, got %d", response.StatusCode())
    }

    if 0 != len(*failureList) {
        t.Fatalf("expected no login failure for accepted credentials, got %d", len(*failureList))
    }
}

/* acceptingAuthenticationRepository answers the one account it holds for its username, and no account for any other. */
type acceptingAuthenticationRepository struct {
    repository.UserRepository
    user *entity.User
}

func (instance *acceptingAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    if nil == instance.user || instance.user.Username != username {
        return nil, false, nil
    }

    return instance.user, true, nil
}

/* postLoginCredentials posts the credentials as the login form sends them and answers the door's response. */
func postLoginCredentials(t *testing.T, runtimeInstance melodyruntimecontract.Runtime, username string, password string) melodyhttpcontract.Response {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username="+username+"&password="+password))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-dispatch-test", time.Now()))

    response, handlerErr := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    return response
}
