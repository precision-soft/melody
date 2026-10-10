package handler

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/repository"
    "github.com/precision-soft/melody/v2/.example/security"
    "github.com/precision-soft/melody/v2/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodyclockcontract "github.com/precision-soft/melody/v2/clock/contract"
    melodyevent "github.com/precision-soft/melody/v2/event"
    melodyeventcontract "github.com/precision-soft/melody/v2/event/contract"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
    melodysession "github.com/precision-soft/melody/v2/session"
    melodysessioncontract "github.com/precision-soft/melody/v2/session/contract"
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

    response, err := LogoutHandler(nil)(nil, httptest.NewRecorder(), request)
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

    response, err := LogoutHandler(nil)(nil, httptest.NewRecorder(), request)
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
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

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

/* the credentials are read from the json body alone: a POST whose query string carries them and whose body names none answers as a request without credentials, where a query read would have authenticated — with the credentials written into every access log in front of the application. The body of the same credentials reaches the authentication, which the empty directory refuses, so the arms are told apart by the status; a media type parameter is read past, and any body other than json is refused 415 before the credentials are read, a cross-site form being unable to post json. */
func TestLoginHandler_ReadsTheJsonCredentialsFromTheBodyAndRefusesAnyOtherBody(t *testing.T) {
    runtimeInstance := loginRuntimeOverAnEmptyDirectory(t)

    login := func(target string, contentType string, body string) (int, string) {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, target, bytes.NewBufferString(body))
        httpRequest.Header.Set("Content-Type", contentType)

        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-form-test", time.Now()))

        response, handlerErr := LoginHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr {
            t.Fatalf("login handler: %v", handlerErr)
        }

        bodyBytes, readErr := io.ReadAll(response.BodyReader())
        if nil != readErr {
            t.Fatalf("read response body: %v", readErr)
        }

        return response.StatusCode(), string(bodyBytes)
    }

    statusCode, body := login("/login?username=admin&password=secret", "application/json", "{}")
    if nethttp.StatusBadRequest != statusCode || false == strings.Contains(body, "invalid credentials input") {
        t.Fatalf("credentials carried by the query were read: status %d, body %s", statusCode, body)
    }

    statusCode, body = login("/login", "application/json", `{"username":"admin","password":"secret"}`)
    if nethttp.StatusUnauthorized != statusCode || false == strings.Contains(body, "invalid credentials") {
        t.Fatalf("credentials carried by the body did not reach the authentication: status %d, body %s", statusCode, body)
    }

    statusCode, body = login("/login", "application/json; charset=utf-8", `{"username":"admin","password":"secret"}`)
    if nethttp.StatusUnauthorized != statusCode {
        t.Fatalf("a json body with a charset parameter did not reach the authentication: status %d, body %s", statusCode, body)
    }

    for _, contentType := range []string{"application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "text/plain", "", "application/jsonp"} {
        statusCode, body = login("/login", contentType, "username=admin&password=secret")
        if nethttp.StatusUnsupportedMediaType != statusCode || false == strings.Contains(body, "the sign-in reads application/json") {
            t.Fatalf("a %q body was not refused 415: status %d, body %s", contentType, statusCode, body)
        }
    }
}

/* registerLoginFailureRecorder registers an event dispatcher that records the failure each security.login.failure event carries, the one listener the login door's refusal must reach; the listener answers listenerErr, so a non-nil one fails the dispatch. A nil logger registers a nop one. */
func registerLoginFailureRecorder(t *testing.T, containerInstance melodycontainercontract.Container, listenerErr error, logger melodyloggingcontract.Logger) *[]error {
    t.Helper()

    failureList := make([]error, 0, 1)

    if nil == logger {
        logger = melodylogging.NewNopLogger()
    }

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
            return logger, nil
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

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString(`{"username":"nobody","password":"wrong"}`))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-failure-test", time.Now()))

    response, handlerErr := LoginHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
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
    journal := &loginJournalRecordingLogger{Logger: melodylogging.NewNopLogger()}
    failureList := registerLoginFailureRecorder(t, containerInstance, errors.New("login failure listener refused"), journal)

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

    dispatchRecords := journal.recordsNamed("security login failure event dispatch failed")
    if 1 != len(dispatchRecords) {
        t.Fatalf("expected the dispatch failure journaled once at error, got %d among %+v", len(dispatchRecords), journal.errorRecords)
    }

    record := dispatchRecords[0]

    if false == strings.Contains(fmt.Sprint(record.context["cause"]), "login failure listener refused") {
        t.Fatalf("expected the record to carry the listener's refusal as its cause, got %v", record.context)
    }

    if 401 != record.context["statusCode"] {
        t.Fatalf("expected the record to name the kept 401, got %v", record.context["statusCode"])
    }
}

type loginJournalRecord struct {
    message string
    context melodyloggingcontract.Context
}

/* loginJournalRecordingLogger keeps the error records a door writes; every other level falls to the embedded nop logger */
type loginJournalRecordingLogger struct {
    melodyloggingcontract.Logger
    errorRecords []loginJournalRecord
}

func (instance *loginJournalRecordingLogger) recordsNamed(message string) []loginJournalRecord {
    named := make([]loginJournalRecord, 0, 1)
    for _, record := range instance.errorRecords {
        if message == record.message {
            named = append(named, record)
        }
    }

    return named
}

func (instance *loginJournalRecordingLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.errorRecords = append(instance.errorRecords, loginJournalRecord{message: message, context: context})
}

/* accepted credentials raise no login failure: the request carries no session, so the door stops after the authentication with its 500, and the only event it could have raised by then is the one this test refuses */
func TestLoginHandler_RaisesNoLoginFailureOnAcceptedCredentials(t *testing.T) {
    passwordHash, hashErr := security.HashPassword("secret")
    if nil != hashErr {
        t.Fatalf("hash password: %v", hashErr)
    }

    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

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

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, username, password))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-dispatch-test", time.Now()))

    response, handlerErr := LoginHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    return response
}

/* directoryAuthenticationRepository answers each account it holds for its username */
type directoryAuthenticationRepository struct {
    repository.UserRepository
    userList []*entity.User
}

func (instance *directoryAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    for _, user := range instance.userList {
        if user.Username == username {
            return user, true, nil
        }
    }

    return nil, false, nil
}

/* cappedLogin is the sign-in door over a session manager, the clock and the in-memory session index, so a sign-in reaches the admission */
type cappedLogin struct {
    runtimeInstance melodyruntimecontract.Runtime
    sessionManager  melodysessioncontract.Manager
    sessionStorage  melodysessioncontract.Storage
    sessionIndex    repository.UserSessionRepository
    clock           *melodyclock.FrozenClock
}

func (instance *cappedLogin) sessionIndexLookup(request melodyhttpcontract.Request) (security.SessionIndex, error) {
    return instance.sessionIndex, nil
}

func newCappedLogin(t *testing.T) *cappedLogin {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    registerLoginFailureRecorder(t, containerInstance, nil, nil)

    userList := make([]*entity.User, 0, 2)
    for index, username := range []string{"user", "editor"} {
        passwordHash, hashErr := security.HashPassword(username)
        if nil != hashErr {
            t.Fatalf("hash password: %v", hashErr)
        }

        userList = append(userList, &entity.User{Id: fmt.Sprintf("user-%d", index+1), Username: username, Password: passwordHash, Roles: []string{"ROLE_USER"}})
    }

    sessionManagerStorage := melodysession.NewInMemoryStorage()
    sessionManager := melodysession.NewManager(sessionManagerStorage, time.Hour)
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return sessionManagerStorage, nil
    })

    melodycontainer.MustRegister(containerInstance, service.ServiceUserService, func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
        return service.NewUserService(&directoryAuthenticationRepository{userList: userList}, &passThroughCache{}, nil), nil
    })
    melodycontainer.MustRegister(containerInstance, melodysession.ServiceSessionManager, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
        return sessionManager, nil
    })
    melodycontainer.MustRegister(containerInstance, melodyhttp.ServiceUrlGenerator, func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.UrlGenerator, error) {
        return melodyhttp.NewUrlGenerator(melodyhttp.NewRouter().RouteRegistry()), nil
    })
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
    melodycontainer.MustRegister(containerInstance, melodyclock.ServiceClock, func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return clockInstance, nil
    })

    return &cappedLogin{
        runtimeInstance: melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
        sessionManager:  sessionManager,
        sessionStorage:  sessionManagerStorage,
        sessionIndex:    repository.NewInMemoryUserSessionRepository(),
        clock:           clockInstance,
    }
}

/* signInAndStore signs the account in on a fresh session and stores the session as the response path would */
func (instance *cappedLogin) signInAndStore(t *testing.T, username string) string {
    t.Helper()

    sessionInstance := instance.signIn(t, username)
    if saveErr := instance.sessionManager.SaveSession(sessionInstance); nil != saveErr {
        t.Fatalf("save session: %v", saveErr)
    }

    return sessionInstance.Id()
}

/* signIn signs the account in on a fresh session and answers the session the response path has not stored yet */
func (instance *cappedLogin) signIn(t *testing.T, username string) melodysessioncontract.Session {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, username, username))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, instance.runtimeInstance, melodyhttp.NewRequestContext("login-cap-test", time.Now()))
    request.Attributes().Set(melodyhttp.RequestAttributeSession, instance.sessionManager.NewSession())

    response, handlerErr := LoginHandler(instance.sessionIndexLookup)(instance.runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the sign-in of %s answered 200, got %v, %v", username, response, handlerErr)
    }

    return getSessionFromRequest(request)
}

/* the cap holds at the sign-in door: the sign-in past repository.UserSessionCap ends the account's oldest session in the storage, keeps its newer ones, and leaves the session of another account alone */
func TestLoginHandler_TheSignInPastTheCapEndsTheAccountsOldestSession(t *testing.T) {
    login := newCappedLogin(t)

    editorSessionId := login.signInAndStore(t, "editor")

    sessionIdList := make([]string, 0, repository.UserSessionCap+1)
    for range repository.UserSessionCap + 1 {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user"))
    }

    if nil != login.sessionManager.Session(sessionIdList[0]) {
        t.Fatalf("expected the oldest session of the account ended by the sign-in past the cap")
    }

    for _, sessionId := range sessionIdList[1:] {
        if nil == login.sessionManager.Session(sessionId) {
            t.Fatalf("expected the account's %d newest sessions kept, %q is gone", repository.UserSessionCap, sessionId)
        }
    }

    if nil == login.sessionManager.Session(editorSessionId) {
        t.Fatal("expected the session of another account untouched by this account's cap")
    }
}

/* a session the sign-out ended gives its place back: with the account at its cap, the sign-out of one session lets the next sign-in in without ending any of the others */
func TestLogoutHandler_TheSignOutGivesTheSessionsPlaceBack(t *testing.T) {
    login := newCappedLogin(t)

    sessionIdList := make([]string, 0, repository.UserSessionCap)
    for range repository.UserSessionCap {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user"))
    }

    signedOutSessionId := sessionIdList[len(sessionIdList)-1]

    logoutRequest := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/logout/", nil), nil, login.runtimeInstance, melodyhttp.NewRequestContext("logout-test", time.Now()))
    logoutRequest.Attributes().Set(melodyhttp.RequestAttributeSession, login.sessionManager.Session(signedOutSessionId))

    if _, logoutErr := LogoutHandler(login.sessionIndexLookup)(login.runtimeInstance, httptest.NewRecorder(), logoutRequest); nil != logoutErr {
        t.Fatalf("logout handler: %v", logoutErr)
    }

    login.signInAndStore(t, "user")

    for _, sessionId := range sessionIdList[:len(sessionIdList)-1] {
        if nil == login.sessionManager.Session(sessionId) {
            t.Fatalf("expected the sign-in after the sign-out to end none of the account's other sessions, %q is gone", sessionId)
        }
    }
}

/* signing in again on the session a client holds retires that session's id and its place with it: the sign-ins on one browser up to the cap end no other session of the account */
func TestLoginHandler_SigningInAgainRetiresThePlaceOfTheRotatedSession(t *testing.T) {
    login := newCappedLogin(t)

    otherSessionId := login.signInAndStore(t, "user")

    sessionInstance := login.sessionManager.NewSession()
    for range repository.UserSessionCap + 1 {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString(`{"username":"user","password":"user"}`))
        httpRequest.Header.Set("Content-Type", "application/json")

        request := melodyhttp.NewRequest(httpRequest, nil, login.runtimeInstance, melodyhttp.NewRequestContext("login-again-test", time.Now()))
        request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

        response, handlerErr := LoginHandler(login.sessionIndexLookup)(login.runtimeInstance, httptest.NewRecorder(), request)
        if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
            t.Fatalf("expected the sign-in answered 200, got %v, %v", response, handlerErr)
        }

        sessionInstance = getSessionFromRequest(request)
        if saveErr := login.sessionManager.SaveSession(sessionInstance); nil != saveErr {
            t.Fatalf("save session: %v", saveErr)
        }
    }

    if nil == login.sessionManager.Session(otherSessionId) {
        t.Fatal("expected the account's other session kept: the sign-ins on one browser hold one place")
    }
}

/* a session that ended elsewhere, its entry lapsed from the storage, gives its place back at the next sign-in: the account's oldest live session is not ended for it */
func TestLoginHandler_ASessionThatEndedElsewhereGivesItsPlaceBack(t *testing.T) {
    login := newCappedLogin(t)

    sessionIdList := make([]string, 0, repository.UserSessionCap)
    for range repository.UserSessionCap {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user"))
    }

    if deleteErr := login.sessionStorage.Delete(sessionIdList[2]); nil != deleteErr {
        t.Fatalf("unexpected error lapsing the session: %v", deleteErr)
    }

    login.clock.Advance(repository.UserSessionAdmissionGrace)

    login.signInAndStore(t, "user")

    for _, sessionId := range []string{sessionIdList[0], sessionIdList[1], sessionIdList[3], sessionIdList[4]} {
        if nil == login.sessionManager.Session(sessionId) {
            t.Fatalf("expected no live session ended while an ended one held a place, %q is gone", sessionId)
        }
    }
}

/* concurrent sign-ins of one account whose sessions the kernel has not stored yet still meet the cap: each row counts as live within the grace, so the sign-ins past the cap end the oldest and no more than repository.UserSessionCap of them can be stored */
func TestLoginHandler_SignInsNotYetStoredStillMeetTheCap(t *testing.T) {
    login := newCappedLogin(t)

    pendingList := make([]melodysessioncontract.Session, 0, repository.UserSessionCap+2)
    for range repository.UserSessionCap + 2 {
        pendingList = append(pendingList, login.signIn(t, "user"))
    }

    storedCount := 0
    for _, sessionInstance := range pendingList {
        if saveErr := login.sessionManager.SaveSession(sessionInstance); nil == saveErr {
            storedCount++
        }
    }

    if repository.UserSessionCap != storedCount {
        t.Fatalf("expected %d of the account's sessions stored, got %d", repository.UserSessionCap, storedCount)
    }
}

/* loginJsonBody is the json body the sign-in door reads */
func loginJsonBody(t *testing.T, username string, password string) *bytes.Buffer {
    t.Helper()

    encoded, encodeErr := json.Marshal(map[string]string{"username": username, "password": password})
    if nil != encodeErr {
        t.Fatalf("encode the sign-in body: %v", encodeErr)
    }

    return bytes.NewBuffer(encoded)
}

/* a form body is no credential refusal: it is answered 415 before the credentials are read, so no security.login.failure is raised for it */
func TestLoginHandler_RefusesAFormBodyWithoutAnnouncingAFailedSignIn(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username=nobody&password=wrong"))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-form-refused-test", time.Now()))

    response, handlerErr := LoginHandler(nil)(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if nethttp.StatusUnsupportedMediaType != response.StatusCode() {
        t.Fatalf("expected the form body refused 415, got %d", response.StatusCode())
    }

    if 0 != len(*failureList) {
        t.Fatalf("expected no security.login.failure for a refused media type, got %d", len(*failureList))
    }
}

/* the sign-in rotates the session id against fixation: the pre-login id a client held is retired from the storage, and the identity is written on the new id the response carries, the pre-login values carried with it */
func TestLoginHandler_RotatesTheSessionIdAndRetiresThePreLoginOne(t *testing.T) {
    login := newCappedLogin(t)

    preLogin := login.sessionManager.NewSession()
    preLogin.Set("example.cart", "three items")
    if saveErr := login.sessionManager.SaveSession(preLogin); nil != saveErr {
        t.Fatalf("save the pre-login session: %v", saveErr)
    }
    preLoginId := preLogin.Id()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, "user", "user"))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, nil, login.runtimeInstance, melodyhttp.NewRequestContext("login-rotation-test", time.Now()))
    request.Attributes().Set(melodyhttp.RequestAttributeSession, login.sessionManager.Session(preLoginId))

    response, handlerErr := LoginHandler(login.sessionIndexLookup)(login.runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the sign-in answered 200, got %v, %v", response, handlerErr)
    }

    rotated := getSessionFromRequest(request)
    if nil == rotated || preLoginId == rotated.Id() {
        t.Fatalf("expected the session id rotated away from %q, got %v", preLoginId, rotated)
    }

    if nil != login.sessionManager.Session(preLoginId) {
        t.Fatalf("expected the pre-login session %q retired from the storage", preLoginId)
    }

    if "" == rotated.String(security.SessionKeySecurityUserId) || "three items" != rotated.String("example.cart") {
        t.Fatalf("expected the identity and the pre-login values on the rotated session, got %v", rotated.All())
    }
}

/* the kernel bounds every body by MELODY_HTTP_MAX_REQUEST_BODY_BYTES; a body past it is the client's too large a payload, answered 413 as the framework answers it on a bind, not as unreadable json */
func TestLoginHandler_AnswersABodyPastTheLimit413(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    recorder := httptest.NewRecorder()
    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, "nobody", "wrong"))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Body = nethttp.MaxBytesReader(recorder, httpRequest.Body, 8)

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-body-limit-test", time.Now()))

    response, handlerErr := LoginHandler(nil)(runtimeInstance, recorder, request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if nethttp.StatusRequestEntityTooLarge != response.StatusCode() {
        t.Fatalf("expected the body past the limit refused 413, got %d", response.StatusCode())
    }

    if 0 != len(*failureList) {
        t.Fatalf("expected no security.login.failure for a body never read, got %d", len(*failureList))
    }
}
