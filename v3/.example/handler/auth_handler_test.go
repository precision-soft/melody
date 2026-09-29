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

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodysessioncontract "github.com/precision-soft/melody/v3/session/contract"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* the cause the login door can really reach names internals: a repository error carries the schema, the table and the database address, and the endpoint is unauthenticated */
const authenticationCauseSecret = "dial tcp 10.0.0.7:3306: connect: connection refused"

type stubAuthenticationEnvironmentSource struct {
    values map[string]string
}

func (instance *stubAuthenticationEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* the repository refuses the username read the authentication performs, which is how a broken backend reaches AuthenticateByUsernameAndPassword on this door */
type refusingAuthenticationRepository struct {
    repository.UserRepository
}

func (instance *refusingAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, errors.New(authenticationCauseSecret)
}

func loginRuntimeForEnvironment(t *testing.T, environmentName string) melodyruntimecontract.Runtime {
    t.Helper()

    source := &stubAuthenticationEnvironmentSource{
        values: map[string]string{
            melodyconfig.EnvKey: environmentName,
        },
    }

    environment, environmentErr := melodyconfig.NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    containerInstance := melodycontainer.NewContainer()

    registerConfigurationErr := melodycontainer.Register[melodyconfigcontract.Configuration](
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
    )
    if nil != registerConfigurationErr {
        t.Fatalf("register configuration: %v", registerConfigurationErr)
    }

    registerServiceErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(&refusingAuthenticationRepository{}, nil, nil), nil
        },
    )
    if nil != registerServiceErr {
        t.Fatalf("register user service: %v", registerServiceErr)
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func loginResponseBody(t *testing.T, runtimeInstance melodyruntimecontract.Runtime) (int, string) {
    t.Helper()

    httpRequest := httptest.NewRequest(
        nethttp.MethodPost,
        "/login",
        bytes.NewBufferString(`{"username":"admin","password":"secret"}`),
    )
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("login-test", time.Now()),
    )

    response, handlerErr := LoginHandler(passwordOnlyLogin())(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }
    if nil == response {
        t.Fatalf("expected a response from the login handler")
    }

    bodyBytes, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read response body: %v", readErr)
    }

    return response.StatusCode(), string(bodyBytes)
}

/* the login door is unauthenticated, so an authentication failure must answer a public message and nothing else: the errors list is written into the response with no debug gate at all, and the causes this call can really reach — a driver error naming the schema and the host — would be handed to anonymous callers verbatim. ApiErrorWithErr is the door that keeps the cause behind the debug gates instead. */
func TestLoginHandler_KeepsTheAuthenticationCauseOutOfTheResponseWithoutDebug(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvProduction)

    statusCode, body := loginResponseBody(t, runtimeInstance)

    if nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("expected the authentication failure answered as 500, got %d", statusCode)
    }

    if true == strings.Contains(body, authenticationCauseSecret) {
        t.Fatalf("expected the cause to stay out of the response, got %s", body)
    }

    var decoded struct {
        Errors  []string         `json:"errors"`
        Context map[string]any   `json:"context"`
        Trace   []map[string]any `json:"trace"`
    }

    if unmarshalErr := json.Unmarshal([]byte(body), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal response body: %v (%s)", unmarshalErr, body)
    }

    if 1 != len(decoded.Errors) || "authentication failed" != decoded.Errors[0] {
        t.Fatalf("expected only the public message in the errors list, got %#v", decoded.Errors)
    }

    if _, exists := decoded.Context["error"]; true == exists {
        t.Fatalf("expected no cause in the context without debug, got %#v", decoded.Context)
    }

    if 0 != len(decoded.Trace) {
        t.Fatalf("expected no trace without debug, got %#v", decoded.Trace)
    }
}

/* the cause is not discarded, it is gated: under the development kernel environment the same call carries it in the debug-gated context and trace, which is what makes the public message safe to keep bare */
func TestLoginHandler_CarriesTheAuthenticationCauseUnderDebug(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvDevelopment)

    statusCode, body := loginResponseBody(t, runtimeInstance)

    if nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("expected the authentication failure answered as 500, got %d", statusCode)
    }

    var decoded struct {
        Errors  []string         `json:"errors"`
        Context map[string]any   `json:"context"`
        Trace   []map[string]any `json:"trace"`
    }

    if unmarshalErr := json.Unmarshal([]byte(body), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal response body: %v (%s)", unmarshalErr, body)
    }

    if 1 != len(decoded.Errors) || "authentication failed" != decoded.Errors[0] {
        t.Fatalf("expected the errors list to stay the public message even under debug, got %#v", decoded.Errors)
    }

    errorEntry, exists := decoded.Context["error"].(map[string]any)
    if false == exists {
        t.Fatalf("expected the cause in the debug-gated context, got %#v", decoded.Context)
    }

    message, isString := errorEntry["message"].(string)
    if false == isString || false == strings.Contains(message, authenticationCauseSecret) {
        t.Fatalf("expected the cause message under debug, got %#v", errorEntry)
    }

    if 0 == len(decoded.Trace) {
        t.Fatalf("expected the unwrap chain under debug")
    }
}

/* The logout doors are asked what the STORAGE holds afterwards, not what the session object says: deleting the two identity keys leaves the entry modified, so the response path saves it back under the same id and re-issues the cookie, and only a cleared session routes that path to DeleteSession. The response path is run here exactly as the kernel runs it, through SaveSession. */
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

/* the credentials are read from the body alone: a POST whose query string carries them and whose form body is empty answers as a request without credentials, where FormValue would have read the query and authenticated — with the credentials written into every access log in front of the application. The body form of the same credentials reaches the authentication, which this fixture's cache refuses, so the two arms are told apart by the status. */
func TestLoginHandler_ReadsTheFormCredentialsFromTheBodyNotTheQuery(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvProduction)

    login := func(target string, body string) (int, string) {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, target, bytes.NewBufferString(body))
        httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-form-test", time.Now()))

        response, handlerErr := LoginHandler(passwordOnlyLogin())(runtimeInstance, httptest.NewRecorder(), request)
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
    if nethttp.StatusInternalServerError != statusCode || false == strings.Contains(body, "authentication failed") {
        t.Fatalf("credentials carried by the body did not reach the authentication: status %d, body %s", statusCode, body)
    }
}

/* absentAuthenticationRepository holds no account, so every credential that reaches the authentication is refused as invalid. */
type absentAuthenticationRepository struct {
    repository.UserRepository
}

func (instance *absentAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, nil
}

/* loginRuntimeRefusingEveryCredential is the runtime over a directory holding no account, with the dispatcher a refused login reaches, and the failures that dispatcher recorded. */
func loginRuntimeRefusingEveryCredential(t *testing.T) (melodyruntimecontract.Runtime, *[]error) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(&absentAuthenticationRepository{}, nil, nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), failureList
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

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username=nobody&password=wrong"))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("login-failure-test", time.Now()))

    response, handlerErr := LoginHandler(passwordOnlyLogin())(runtimeInstance, httptest.NewRecorder(), request)
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
            return service.NewUserService(&acceptingAuthenticationRepository{}, nil, nil), nil
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
                nil,
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

    response, handlerErr := LoginHandler(passwordOnlyLogin())(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    return response
}

/* passwordOnlyLogin is the sign-in chain of an application with no second factor wired: the password alone, checked through the user service of the request's container. */
func passwordOnlyLogin() LoginAuthenticator {
    return security.NewLoginAuthentication(
        melodysecurity.NewAuthenticatorManager(security.NewPasswordAuthenticator(checkPasswordThroughUserService)),
        nil,
    )
}

func checkPasswordThroughUserService(runtimeInstance melodyruntimecontract.Runtime, username string, password string) (*entity.User, bool, error) {
    return service.MustGetUserService(runtimeInstance.Container()).AuthenticateByUsernameAndPassword(runtimeInstance.Context(), username, password)
}

const (
    secondFactorTestSecret       = "JBSWY3DPEHPK3PXP"
    secondFactorTestRecoveryCode = "recovery-code-one"
    secondFactorTestAllowance    = 3
)

/* enrollmentDouble holds the one enrollment the second-factor pins sign in against, and redeems each recovery code once. */
type enrollmentDouble struct {
    enrolled      map[string]string
    recoveryCodes map[string][]string
}

func (instance *enrollmentDouble) FindTotpSecret(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string) (string, bool, error) {
    secret, enrolled := instance.enrolled[userIdentifier]

    return secret, enrolled, nil
}

func (instance *enrollmentDouble) RedeemRecoveryCode(runtimeInstance melodyruntimecontract.Runtime, userIdentifier string, code string) (bool, error) {
    remaining := make([]string, 0, len(instance.recoveryCodes[userIdentifier]))
    redeemed := false

    for _, candidate := range instance.recoveryCodes[userIdentifier] {
        if candidate == code && false == redeemed {
            redeemed = true

            continue
        }

        remaining = append(remaining, candidate)
    }

    instance.recoveryCodes[userIdentifier] = remaining

    return redeemed, nil
}

/* secondFactorLogin is the sign-in as the application wires it with a database: the password, the per-account budget in front of the second factor, and the framework's TOTP authenticator over the enrollments, on a frozen clock. The editor is enrolled; the user is not. */
type secondFactorLogin struct {
    runtimeInstance melodyruntimecontract.Runtime
    authentication  LoginAuthenticator
    failureList     *[]error
    clock           *melodyclock.FrozenClock
    sessionManager  melodysessioncontract.Manager
}

func newSecondFactorLogin(t *testing.T) *secondFactorLogin {
    t.Helper()

    editorHash, editorHashErr := security.HashPassword("editor")
    if nil != editorHashErr {
        t.Fatalf("hash password: %v", editorHashErr)
    }

    userHash, userHashErr := security.HashPassword("user")
    if nil != userHashErr {
        t.Fatalf("hash password: %v", userHashErr)
    }

    containerInstance := melodycontainer.NewContainer()
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

    accounts := &accountDirectory{
        accountList: []*entity.User{
            {Id: "user-2", Username: "editor", Password: editorHash, Roles: []string{"ROLE_EDITOR"}},
            {Id: "user-1", Username: "user", Password: userHash, Roles: []string{"ROLE_USER"}},
        },
    }

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(accounts, nil, nil), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    sessionManager := melodysession.NewManager(melodysession.NewInMemoryStorage(), time.Hour)
    registerSessionErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return sessionManager, nil
        },
    )
    if nil != registerSessionErr {
        t.Fatalf("register session manager: %v", registerSessionErr)
    }

    registerUrlGeneratorErr := melodycontainer.Register[melodyhttpcontract.UrlGenerator](
        containerInstance,
        melodyhttp.ServiceUrlGenerator,
        func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.UrlGenerator, error) {
            return melodyhttp.NewUrlGenerator(melodyhttp.NewRouter().RouteRegistry()), nil
        },
    )
    if nil != registerUrlGeneratorErr {
        t.Fatalf("register url generator: %v", registerUrlGeneratorErr)
    }

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))

    password := security.NewPasswordAuthenticator(checkPasswordThroughUserService)
    budget := security.NewSecondFactorBudget(
        password,
        melodyhttpmiddleware.NewSlidingWindowLimiterWithClock(clockInstance, secondFactorTestAllowance, 15*time.Minute),
        melodysecurity.DefaultTotpCodeHeaderName,
        melodysecurity.DefaultTotpRecoveryHeaderName,
    )

    secondFactor := melodysecurity.NewTotpSecondFactorAuthenticator(
        melodysecurity.TotpSecondFactorAuthenticatorConfig{
            Primary: budget,
            Enrollments: &enrollmentDouble{
                enrolled:      map[string]string{"user-2": secondFactorTestSecret},
                recoveryCodes: map[string][]string{"user-2": {secondFactorTestRecoveryCode}},
            },
            ReplayGuard: melodysecurity.NewMemoryNonceGuardWithClock(clockInstance),
            Clock:       clockInstance,
        },
    )

    return &secondFactorLogin{
        runtimeInstance: melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
        authentication:  security.NewLoginAuthentication(melodysecurity.NewAuthenticatorManager(secondFactor), budget),
        failureList:     failureList,
        clock:           clockInstance,
        sessionManager:  sessionManager,
    }
}

/* post signs in with the credentials and the second-factor headers given, on a request carrying a fresh session, and answers the response with the session the request carries afterwards. */
func (instance *secondFactorLogin) post(t *testing.T, username string, password string, headerList map[string]string) (melodyhttpcontract.Response, melodysessioncontract.Session) {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username="+username+"&password="+password))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    for name, value := range headerList {
        httpRequest.Header.Set(name, value)
    }

    request := melodyhttp.NewRequest(httpRequest, nil, instance.runtimeInstance, melodyhttp.NewRequestContext("login-second-factor-test", time.Now()))
    request.Attributes().Set(melodyhttp.RequestAttributeSession, instance.sessionManager.NewSession())

    response, handlerErr := LoginHandler(instance.authentication)(instance.runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if true == request.Attributes().Has(security.RequestAttributeLoginPassword) {
        t.Fatal("expected the password to leave the request once the authenticators read it")
    }

    return response, getSessionFromRequest(request)
}

func (instance *secondFactorLogin) currentCode(t *testing.T) string {
    t.Helper()

    code, codeErr := totp.GenerateCodeAt(secondFactorTestSecret, instance.clock.Now(), totp.Config{})
    if nil != codeErr {
        t.Fatalf("generate code: %v", codeErr)
    }

    return code
}

/* requireLoginAnswer reads the body once and requires the status and every fragment in it. */
func requireLoginAnswer(t *testing.T, response melodyhttpcontract.Response, statusCode int, fragmentList ...string) {
    t.Helper()

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read response body: %v", readErr)
    }

    if statusCode != response.StatusCode() {
        t.Fatalf("expected %d, got %d %s", statusCode, response.StatusCode(), body)
    }

    for _, fragment := range fragmentList {
        if false == strings.Contains(string(body), fragment) {
            t.Fatalf("expected the %d answer to carry %q, got %s", statusCode, fragment, body)
        }
    }
}

/* an accepted password of an enrolled account, with no code, is the challenge: 401 naming the factor, and no login failure, since nothing is refused */
func TestLoginHandler_AnswersTheChallengeWithoutAFailureForAnEnrolledAccountWithoutACode(t *testing.T) {
    login := newSecondFactorLogin(t)

    response, _ := login.post(t, "editor", "editor", nil)

    requireLoginAnswer(t, response, nethttp.StatusUnauthorized, `"factor":"totp"`, "second factor required")

    if 0 != len(*login.failureList) {
        t.Fatalf("expected no login failure for an outstanding second factor, got %d", len(*login.failureList))
    }
}

/* a wrong code is a refused sign-in: the same 401 a refused password answers, and one login failure */
func TestLoginHandler_RefusesAWrongCodeAsAFailedSignIn(t *testing.T) {
    login := newSecondFactorLogin(t)

    wrongCode := "000000"
    if wrongCode == login.currentCode(t) {
        wrongCode = "111111"
    }

    response, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: wrongCode})

    requireLoginAnswer(t, response, nethttp.StatusUnauthorized, "invalid credentials")

    if 1 != len(*login.failureList) {
        t.Fatalf("expected one login failure for a refused code, got %d", len(*login.failureList))
    }
}

/* the right code signs the enrolled account in under its own identity; the same code presented again is a replay and is refused */
func TestLoginHandler_SignsInWithTheRightCodeOnceAndRefusesItsReplay(t *testing.T) {
    login := newSecondFactorLogin(t)
    code := login.currentCode(t)

    response, sessionInstance := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code})

    requireLoginAnswer(t, response, nethttp.StatusOK, "redirectUrl")

    if "user-2" != sessionInstance.String(security.SessionKeySecurityUserId) {
        t.Fatalf("expected the session to name user-2, got %q", sessionInstance.String(security.SessionKeySecurityUserId))
    }

    replay, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: code})

    requireLoginAnswer(t, replay, nethttp.StatusUnauthorized, "invalid credentials")

    if 1 != len(*login.failureList) {
        t.Fatalf("expected one login failure, for the replay alone, got %d", len(*login.failureList))
    }
}

/* a recovery code signs the enrolled account in once; spent, it is refused */
func TestLoginHandler_SignsInWithARecoveryCodeOnce(t *testing.T) {
    login := newSecondFactorLogin(t)
    recoveryHeader := map[string]string{melodysecurity.DefaultTotpRecoveryHeaderName: secondFactorTestRecoveryCode}

    first, _ := login.post(t, "editor", "editor", recoveryHeader)
    requireLoginAnswer(t, first, nethttp.StatusOK, "redirectUrl")

    second, _ := login.post(t, "editor", "editor", recoveryHeader)
    requireLoginAnswer(t, second, nethttp.StatusUnauthorized, "invalid credentials")
}

/* an account with no enrollment signs in on its password alone, as before the second factor was wired */
func TestLoginHandler_SignsInAnAccountWithoutEnrollmentOnItsPassword(t *testing.T) {
    login := newSecondFactorLogin(t)

    response, sessionInstance := login.post(t, "user", "user", nil)

    requireLoginAnswer(t, response, nethttp.StatusOK, "redirectUrl")

    if "user-1" != sessionInstance.String(security.SessionKeySecurityUserId) {
        t.Fatalf("expected the session to name user-1, got %q", sessionInstance.String(security.SessionKeySecurityUserId))
    }
}

/* once an account has presented its budget of codes, even the right code is refused 429 before it is read; a caller without the password spends nothing of it, and a right code gives the budget back */
func TestLoginHandler_RefusesTheRightCodeOnceTheAccountSpentItsBudget(t *testing.T) {
    login := newSecondFactorLogin(t)

    wrongCode := "000000"
    if wrongCode == login.currentCode(t) {
        wrongCode = "111111"
    }

    for attempt := 0; attempt < secondFactorTestAllowance+2; attempt++ {
        refused, _ := login.post(t, "editor", "wrong-password", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: wrongCode})
        requireLoginAnswer(t, refused, nethttp.StatusUnauthorized, "invalid credentials")
    }

    for attempt := 0; attempt < secondFactorTestAllowance; attempt++ {
        refused, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: wrongCode})
        requireLoginAnswer(t, refused, nethttp.StatusUnauthorized, "invalid credentials")
    }

    spent, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})
    requireLoginAnswer(t, spent, nethttp.StatusTooManyRequests, "too many attempts")

    challenge, _ := login.post(t, "editor", "editor", nil)
    requireLoginAnswer(t, challenge, nethttp.StatusUnauthorized, "second factor required")
}

/* a right code gives the account its budget back, so the codes mistyped before it do not count against the next sign-in */
func TestLoginHandler_GivesTheBudgetBackOnAnAcceptedCode(t *testing.T) {
    login := newSecondFactorLogin(t)

    wrongCode := "000000"
    if wrongCode == login.currentCode(t) {
        wrongCode = "111111"
    }

    for attempt := 0; attempt < secondFactorTestAllowance-1; attempt++ {
        refused, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: wrongCode})
        requireLoginAnswer(t, refused, nethttp.StatusUnauthorized, "invalid credentials")
    }

    accepted, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})
    requireLoginAnswer(t, accepted, nethttp.StatusOK, "redirectUrl")

    for attempt := 0; attempt < secondFactorTestAllowance-1; attempt++ {
        refused, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: wrongCode})
        requireLoginAnswer(t, refused, nethttp.StatusUnauthorized, "invalid credentials")
    }

    login.clock.Advance(30 * time.Second)

    again, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})
    requireLoginAnswer(t, again, nethttp.StatusOK, "redirectUrl")
}

/* accountDirectory answers the accounts it holds by username. */
type accountDirectory struct {
    repository.UserRepository
    accountList []*entity.User
}

func (instance *accountDirectory) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    for _, account := range instance.accountList {
        if username == account.Username {
            return account, true, nil
        }
    }

    return nil, false, nil
}
