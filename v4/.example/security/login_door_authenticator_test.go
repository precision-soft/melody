package security_test

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/presenter"
    "github.com/precision-soft/melody/v4/.example/repository"
    "github.com/precision-soft/melody/v4/.example/route"
    "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/service"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodyconfigcontract "github.com/precision-soft/melody/v4/config/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyevent "github.com/precision-soft/melody/v4/event"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    melodykernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/security/totp"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

const authenticationCauseSecret = "dial tcp 10.0.0.7:3306: connect: connection refused"

type stubAuthenticationEnvironmentSource struct {
    values map[string]string
}

func (instance *stubAuthenticationEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

type refusingAuthenticationRepository struct {
    repository.UserRepository
}

func (instance *refusingAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, errors.New(authenticationCauseSecret)
}

func loginRuntimeForEnvironment(t *testing.T, environmentName string) melodycontainercontract.Runtime {
    t.Helper()

    runtimeInstance, _ := loginRuntimeForEnvironmentRecordingFailures(t, environmentName)

    return runtimeInstance
}

func loginRuntimeForEnvironmentRecordingFailures(t *testing.T, environmentName string) (melodycontainercontract.Runtime, *[]error) {
    t.Helper()

    source := &stubAuthenticationEnvironmentSource{
        values: map[string]string{
            melodyconfig.EnvironmentNameKey: environmentName,
        },
    }

    environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), source)
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, t.TempDir(), melodyconfig.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

    registerConfigurationErr := melodycontainer.Register[melodyconfigcontract.Configuration](
        containerInstance,
        melodyconfig.ServiceConfig,
        func(resolver melodycontainercontract.Resolver) (melodyconfigcontract.Configuration, error) {
            return configuration, nil
        },
        melodycontainer.RegisterOptions{},
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
        melodycontainer.RegisterOptions{},
    )
    if nil != registerServiceErr {
        t.Fatalf("register user service: %v", registerServiceErr)
    }

    return melodyruntime.NewRequest(context.Background(), containerInstance.NewScope()), failureList
}

func loginResponseBody(t *testing.T, runtimeInstance melodycontainercontract.Runtime) (int, string) {
    t.Helper()

    httpRequest := httptest.NewRequest(
        nethttp.MethodPost,
        "/login",
        bytes.NewBufferString(`{"username":"admin","password":"secret"}`),
    )
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(
        httpRequest,
        runtimeInstance,
        melodyhttp.RequestConfig{
            RequestContext: melodyhttp.NewRequestContext("login-test", time.Now()),
        },
    )
    publishSignInSession(t, request)

    response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
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

func TestLoginHandler_KeepsTheAuthenticationCauseOutOfTheResponseWithoutDebug(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvironmentProduction)

    statusCode, body := loginResponseBody(t, runtimeInstance)

    if nethttp.StatusServiceUnavailable != statusCode {
        t.Fatalf("expected the account store outage answered as 503, got %d", statusCode)
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

    if 1 != len(decoded.Errors) || "service unavailable" != decoded.Errors[0] {
        t.Fatalf("expected only the public message in the errors list, got %#v", decoded.Errors)
    }

    if _, exists := decoded.Context["error"]; true == exists {
        t.Fatalf("expected no cause in the context without debug, got %#v", decoded.Context)
    }

    if 0 != len(decoded.Trace) {
        t.Fatalf("expected no trace without debug, got %#v", decoded.Trace)
    }
}

func TestLoginHandler_CarriesTheAuthenticationCauseUnderDebug(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvironmentDevelopment)

    statusCode, body := loginResponseBody(t, runtimeInstance)

    if nethttp.StatusServiceUnavailable != statusCode {
        t.Fatalf("expected the account store outage answered as 503, got %d", statusCode)
    }

    var decoded struct {
        Errors  []string         `json:"errors"`
        Context map[string]any   `json:"context"`
        Trace   []map[string]any `json:"trace"`
    }

    if unmarshalErr := json.Unmarshal([]byte(body), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal response body: %v (%s)", unmarshalErr, body)
    }

    if 1 != len(decoded.Errors) || "service unavailable" != decoded.Errors[0] {
        t.Fatalf("expected the errors list to stay the public message even under debug, got %#v", decoded.Errors)
    }

    if _, exists := decoded.Context["error"].(map[string]any); false == exists {
        t.Fatalf("expected the error in the debug-gated context, got %#v", decoded.Context)
    }

    if false == strings.Contains(body, authenticationCauseSecret) {
        t.Fatalf("expected the cause under debug, got %s", body)
    }

    if 0 == len(decoded.Trace) {
        t.Fatalf("expected the unwrap chain under debug")
    }
}

func TestLoginHandler_ReadsTheJsonCredentialsFromTheBodyAndRefusesAnyOtherBody(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeForEnvironmentRecordingFailures(t, melodyconfig.EnvironmentProduction)

    login := func(target string, contentType string, body string) (int, string) {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, target, bytes.NewBufferString(body))
        httpRequest.Header.Set("Content-Type", contentType)

        request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-form-test", time.Now())})
        publishSignInSession(t, request)

        response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
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

    if 0 != len(*failureList) {
        t.Fatalf("expected a malformed input counted as no failed sign-in, got %v", *failureList)
    }

    statusCode, body = login("/login", "application/json", `{"username":"admin","password":"secret"}`)
    if nethttp.StatusServiceUnavailable != statusCode || false == strings.Contains(body, "service unavailable") {
        t.Fatalf("credentials carried by the body did not reach the authentication: status %d, body %s", statusCode, body)
    }

    statusCode, body = login("/login", "application/json; charset=utf-8", `{"username":"admin","password":"secret"}`)
    if nethttp.StatusServiceUnavailable != statusCode {
        t.Fatalf("a json body with a charset parameter did not reach the authentication: status %d, body %s", statusCode, body)
    }

    for _, contentType := range []string{"application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "text/plain", "", "application/jsonp"} {
        statusCode, body = login("/login", contentType, "username=admin&password=secret")
        if nethttp.StatusUnsupportedMediaType != statusCode || false == strings.Contains(body, "the sign-in reads application/json") {
            t.Fatalf("a %q body was not refused 415: status %d, body %s", contentType, statusCode, body)
        }
    }

    if 0 != len(*failureList) {
        t.Fatalf("expected neither an outage nor a refused media type counted as a failed sign-in, got %v", *failureList)
    }
}

type absentAuthenticationRepository struct {
    repository.UserRepository
}

func (instance *absentAuthenticationRepository) FindByUsername(ctx context.Context, username string) (*entity.User, bool, error) {
    return nil, false, nil
}

func loginRuntimeRefusingEveryCredential(t *testing.T) (melodycontainercontract.Runtime, *[]error) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, nil)

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(&absentAuthenticationRepository{}, nil, nil), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    return melodyruntime.NewRequest(context.Background(), containerInstance.NewScope()), failureList
}

func registerLoginFailureRecorder(t *testing.T, containerInstance melodycontainercontract.Container, listenerErr error, logger melodyloggingcontract.Logger) *[]error {
    t.Helper()

    failureList := make([]error, 0, 1)

    if nil == logger {
        logger = melodylogging.NewNopLogger()
    }

    dispatcher := melodyevent.NewEventDispatcher(melodyevent.EventDispatcherConfig{Clock: melodyclock.NewSystemClock()})
    dispatcher.AddListener(
        melodysecuritycontract.EventSecurityLoginFailure,
        func(runtimeInstance melodycontainercontract.Runtime, eventValue melodyeventcontract.Event) error {
            if failure, isFailure := eventValue.Payload().(*melodysecurity.LoginFailureEvent); true == isFailure {
                failureList = append(failureList, failure.Error())
            }

            return listenerErr
        },
        melodyeventcontract.ListenerConfig{},
    )

    registerErr := melodycontainer.Register[melodyeventcontract.EventDispatcher](
        containerInstance,
        melodyevent.ServiceEventDispatcher,
        func(resolver melodycontainercontract.Resolver) (melodyeventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register event dispatcher: %v", registerErr)
    }

    registerLoggerErr := melodycontainer.Register[melodyloggingcontract.Logger](
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return logger, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerLoggerErr {
        t.Fatalf("register logger: %v", registerLoggerErr)
    }

    return &failureList
}

func TestLoginHandler_RaisesTheLoginFailureOnRefusedCredentials(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString(`{"username":"nobody","password":"wrong"}`))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-failure-test", time.Now())})
    publishSignInSession(t, request)

    response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected the refused login answered 401, got %d", response.StatusCode())
    }

    if 1 != len(*failureList) {
        t.Fatalf("expected one security.login.failure event, got %d", len(*failureList))
    }

    if "invalid credentials" != (*failureList)[0].Error() {
        t.Fatalf("expected the event to carry the invalid credentials failure, got %v", (*failureList)[0])
    }

    if true == strings.Contains((*failureList)[0].Error(), "wrong") || true == strings.Contains((*failureList)[0].Error(), "nobody") {
        t.Fatalf("expected the failure to name neither credential, got %q", (*failureList)[0].Error())
    }
}

func TestLoginHandler_KeepsTheRefusalWhenTheLoginFailureDispatchFails(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    journal := &loginJournalRecordingLogger{Logger: melodylogging.NewNopLogger()}
    failureList := registerLoginFailureRecorder(t, containerInstance, errors.New("login failure listener refused"), journal)

    registerErr := melodycontainer.Register[*service.UserService](
        containerInstance,
        service.ServiceUserService,
        func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
            return service.NewUserService(&acceptingAuthenticationRepository{}, nil, nil), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

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

    if false == strings.Contains(fmt.Sprint(record.context["causeChain"]), "login failure listener refused") {
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

func TestLoginHandler_AMissingSessionRefusesTheSignInBeforeTheCredentials(t *testing.T) {
    passwordHash, hashErr := security.HashPassword("secret")
    if nil != hashErr {
        t.Fatalf("hash password: %v", hashErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
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
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, "admin", "secret"))
    httpRequest.Header.Set("Content-Type", "application/json")

    response, handlerErr := serveLoginDoor(t, runtimeInstance, melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-no-session-test", time.Now())}), passwordOnlyLogin(), nil)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected the sign-in to stop at the missing session with 500, got %d", response.StatusCode())
    }

    if 0 != len(*failureList) {
        t.Fatalf("expected no failed sign-in raised for credentials the door never checked, got %d", len(*failureList))
    }
}

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

func postLoginCredentials(t *testing.T, runtimeInstance melodycontainercontract.Runtime, username string, password string) melodyhttpcontract.Response {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, username, password))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-dispatch-test", time.Now())})
    publishSignInSession(t, request)

    response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
    if nil != handlerErr {
        t.Fatalf("login handler: %v", handlerErr)
    }

    return response
}

type doorDispatcher struct {
    melodyeventcontract.EventDispatcher
    listener melodyeventcontract.EventListener
}

func (instance *doorDispatcher) AddListener(eventName string, listener melodyeventcontract.EventListener, config melodyeventcontract.ListenerConfig) melodyeventcontract.ListenerRegistration {
    instance.listener = listener

    return melodyeventcontract.ListenerRegistration{}
}

type doorKernel struct {
    melodykernelcontract.Kernel
    dispatcher *doorDispatcher
}

func (instance *doorKernel) EventDispatcher() melodyeventcontract.EventDispatcher {
    return instance.dispatcher
}

type anonymousDoorTokenSource struct{}

func (instance *anonymousDoorTokenSource) Name() string { return "anonymous" }

func (instance *anonymousDoorTokenSource) Stateless() bool {
    return false
}

func (instance *anonymousDoorTokenSource) Resolve(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    return melodysecurity.NewAnonymousToken(), nil
}

func serveLoginDoor(t *testing.T, runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request, chain security.LoginChain, sessionIndex security.SessionIndexLookup) (melodyhttpcontract.Response, error) {
    t.Helper()

    authenticator := &security.LoginDoorAuthenticator{}
    authenticator.Bind(chain)

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:        "main",
        Matcher:     melodysecurity.NewPathPrefixMatcher("/"),
        TokenSource: &anonymousDoorTokenSource{},
        LoginPath:   request.HttpRequest().URL.Path,
        LoginHandler: security.NewSessionLoginHandler(
            func(runtimeInstance melodycontainercontract.Runtime, userId string) (*entity.User, bool, error) {
                account, known := security.LoginAccount(request)

                return account, known, nil
            },
            sessionIndex,
        ),
        LoginAuthenticator: authenticator,
    })

    dispatcher := &doorDispatcher{}
    melodysecurity.RegisterKernelSecurityResolutionListener(
        &doorKernel{dispatcher: dispatcher},
        melodysecurity.NewFirewallRegistry(melodysecurity.NewCompiledConfiguration([]*melodysecurity.CompiledFirewall{firewall}, nil)),
    )

    requestEvent := melodyhttp.NewKernelRequestEvent(runtimeInstance, request)
    if listenerErr := dispatcher.listener(runtimeInstance, melodyevent.NewEvent(melodykernelcontract.EventKernelRequest, requestEvent, melodyclock.NewSystemClock().Now())); nil != listenerErr {
        return nil, listenerErr
    }

    if nil != requestEvent.Refusal() {
        return presenter.ApiRefusalOfError(runtimeInstance, request, requestEvent.Refusal(), melodyconfig.DefaultUnavailableRetryAfter), nil
    }

    return requestEvent.Response(), nil
}

func getSessionFromRequest(request melodyhttpcontract.Request) melodysessioncontract.Session {
    sessionInstance, sessionErr := melodyhttp.RequestSession(request)
    if nil != sessionErr {
        return nil
    }

    return sessionInstance
}

func publishSignInSession(t *testing.T, request melodyhttpcontract.Request) {
    t.Helper()

    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{}), melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})
    if publishErr := melodyhttp.SetRequestSession(request, mustSession(manager.NewSession(exampleTestRuntime()))); nil != publishErr {
        t.Fatalf("publish the session: %v", publishErr)
    }
}

func passwordOnlyLogin() security.LoginChain {
    return security.NewLoginAuthentication(
        melodysecurity.NewAuthenticatorManager([]melodysecuritycontract.Authenticator{security.NewPasswordAuthenticator(checkPasswordThroughUserService)}),
        nil,
    )
}

func checkPasswordThroughUserService(runtimeInstance melodycontainercontract.Runtime, username string, password string) (*entity.User, bool, error) {
    return service.MustGetUserService(runtimeInstance.Context(), runtimeInstance.Scope()).AuthenticateByUsernameAndPassword(runtimeInstance.Context(), username, password)
}

const (
    secondFactorTestSecret       = "JBSWY3DPEHPK3PXP"
    secondFactorTestRecoveryCode = "recovery-code-one"
    secondFactorTestAllowance    = 3
)

type enrollmentDouble struct {
    enrolled      map[string]string
    recoveryCodes map[string][]string
}

func (instance *enrollmentDouble) FindTotpSecret(runtimeInstance melodycontainercontract.Runtime, userIdentifier string) (string, bool, error) {
    secret, enrolled := instance.enrolled[userIdentifier]

    return secret, enrolled, nil
}

func (instance *enrollmentDouble) RedeemRecoveryCode(runtimeInstance melodycontainercontract.Runtime, userIdentifier string, code string) (bool, error) {
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

type secondFactorLogin struct {
    runtimeInstance melodycontainercontract.Runtime
    authentication  security.LoginChain
    failureList     *[]error
    clock           *melodyclock.FrozenClock
    sessionManager  melodysessioncontract.Manager
    sessionStorage  melodysessioncontract.Storage
    sessionIndex    repository.UserSessionRepository
}

func (instance *secondFactorLogin) sessionIndexLookup(runtimeInstance melodycontainercontract.Runtime) (security.SessionIndex, error) {
    return instance.sessionIndex, nil
}

type secondFactorLoginOptions struct {
    limiter     func(clockInstance melodyclockcontract.Clock) melodyhttpcontract.RateLimiter
    enrollments melodysecuritycontract.TwoFactorEnrollmentStore
    logger      melodyloggingcontract.Logger
}

func newSecondFactorLogin(t *testing.T) *secondFactorLogin {
    t.Helper()

    return newSecondFactorLoginWith(t, secondFactorLoginOptions{})
}

func newSecondFactorLoginWith(t *testing.T, options secondFactorLoginOptions) *secondFactorLogin {
    t.Helper()

    editorHash, editorHashErr := security.HashPassword("editor")
    if nil != editorHashErr {
        t.Fatalf("hash password: %v", editorHashErr)
    }

    userHash, userHashErr := security.HashPassword("user")
    if nil != userHashErr {
        t.Fatalf("hash password: %v", userHashErr)
    }

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    failureList := registerLoginFailureRecorder(t, containerInstance, nil, options.logger)

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
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register user service: %v", registerErr)
    }

    sessionManagerStorage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    sessionManager := melodysession.NewManager(sessionManagerStorage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return sessionManagerStorage, nil
    }, melodycontainer.RegisterOptions{})
    registerSessionErr := melodycontainer.Register[melodysessioncontract.Manager](
        containerInstance,
        melodysession.ServiceSessionManager,
        func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
            return sessionManager, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerSessionErr {
        t.Fatalf("register session manager: %v", registerSessionErr)
    }

    registerUrlGeneratorErr := melodycontainer.Register[melodyhttpcontract.UrlGenerator](
        containerInstance,
        melodyhttp.ServiceUrlGenerator,
        func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.UrlGenerator, error) {
            return melodyhttp.NewUrlGenerator(landingPageRouter().RouteRegistry(), melodyhttp.UrlGeneratorConfig{}), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerUrlGeneratorErr {
        t.Fatalf("register url generator: %v", registerUrlGeneratorErr)
    }

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
    registerClockErr := melodycontainer.Register[melodyclockcontract.Clock](
        containerInstance,
        melodyclock.ServiceClock,
        func(resolver melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
            return clockInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerClockErr {
        t.Fatalf("register clock: %v", registerClockErr)
    }

    sessionIndex, sessionIndexErr := repository.NewUserSessionRepository(persistence.NewCatalogStorage(nil), &persistence.CatalogSchema{})
    if nil != sessionIndexErr {
        t.Fatalf("new session index: %v", sessionIndexErr)
    }

    var limiter melodyhttpcontract.RateLimiter = melodyhttpmiddleware.NewSlidingWindowLimiter("test", secondFactorTestAllowance, 15 * time.Minute, melodyhttpmiddleware.LimiterConfig{Clock: clockInstance})
    if nil != options.limiter {
        limiter = options.limiter(clockInstance)
    }

    var enrollments melodysecuritycontract.TwoFactorEnrollmentStore = &enrollmentDouble{
        enrolled:      map[string]string{"user-2": secondFactorTestSecret},
        recoveryCodes: map[string][]string{"user-2": {secondFactorTestRecoveryCode}},
    }
    if nil != options.enrollments {
        enrollments = options.enrollments
    }

    password := security.NewPasswordAuthenticator(checkPasswordThroughUserService)
    budget := security.NewSecondFactorBudget(
        password,
        limiter,
        melodysecurity.DefaultTotpCodeHeaderName,
        melodysecurity.DefaultTotpRecoveryHeaderName,
    )

    failedAttemptCache := melodycache.NewManager(
        melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{Clock: clockInstance}),
        melodycache.NewJsonSerializer(),
        melodycache.ManagerConfig{OwnsBackend: true},
    )
    t.Cleanup(func() {
        _ = failedAttemptCache.CloseWithContext(context.Background())
    })

    secondFactor := melodysecurity.NewTotpSecondFactorAuthenticator(
        budget, enrollments, melodysecurity.TotpSecondFactorAuthenticatorConfig{
            ReplayGuard:        melodysecurity.NewMemoryNonceGuard(melodysecurity.MemoryNonceGuardConfig{Clock: clockInstance}),
            Clock:              clockInstance,
            FailedAttemptStore: melodysecurity.NewCacheFailedAttemptStore(melodysecurity.CacheFailedAttemptStoreConfig{Cache: failedAttemptCache, Clock: clockInstance}),
        },
    )

    return &secondFactorLogin{
        runtimeInstance: melodyruntime.NewRequest(context.Background(), containerInstance.NewScope()),
        authentication:  security.NewLoginAuthentication(melodysecurity.NewAuthenticatorManager([]melodysecuritycontract.Authenticator{secondFactor}), budget),
        failureList:     failureList,
        clock:           clockInstance,
        sessionManager:  sessionManager,
        sessionStorage:  sessionManagerStorage,
        sessionIndex:    sessionIndex,
    }
}

func (instance *secondFactorLogin) post(t *testing.T, username string, password string, headerList map[string]string) (melodyhttpcontract.Response, melodysessioncontract.Session) {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, username, password))
    httpRequest.Header.Set("Content-Type", "application/json")
    for name, value := range headerList {
        httpRequest.Header.Set(name, value)
    }

    request := melodyhttp.NewRequest(httpRequest, instance.runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-second-factor-test", time.Now())})
    _ = melodyhttp.SetRequestSession(request, mustSession(instance.sessionManager.NewSession(exampleTestRuntime())))

    response, handlerErr := serveLoginDoor(t, instance.runtimeInstance, request, instance.authentication, instance.sessionIndexLookup)
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

func TestLoginHandler_AnswersTheChallengeWithoutAFailureForAnEnrolledAccountWithoutACode(t *testing.T) {
    login := newSecondFactorLogin(t)

    response, _ := login.post(t, "editor", "editor", nil)

    requireLoginAnswer(t, response, nethttp.StatusUnauthorized, `"factor":"totp"`, "second factor required")

    if 0 != len(*login.failureList) {
        t.Fatalf("expected no login failure for an outstanding second factor, got %d", len(*login.failureList))
    }
}

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

func TestLoginHandler_SignsInWithARecoveryCodeOnce(t *testing.T) {
    login := newSecondFactorLogin(t)
    recoveryHeader := map[string]string{melodysecurity.DefaultTotpRecoveryHeaderName: secondFactorTestRecoveryCode}

    first, _ := login.post(t, "editor", "editor", recoveryHeader)
    requireLoginAnswer(t, first, nethttp.StatusOK, "redirectUrl")

    second, _ := login.post(t, "editor", "editor", recoveryHeader)
    requireLoginAnswer(t, second, nethttp.StatusUnauthorized, "invalid credentials")
}

func TestLoginHandler_SignsInAnAccountWithoutEnrollmentOnItsPassword(t *testing.T) {
    login := newSecondFactorLogin(t)

    response, sessionInstance := login.post(t, "user", "user", nil)

    requireLoginAnswer(t, response, nethttp.StatusOK, "redirectUrl")

    if "user-1" != sessionInstance.String(security.SessionKeySecurityUserId) {
        t.Fatalf("expected the session to name user-1, got %q", sessionInstance.String(security.SessionKeySecurityUserId))
    }
}

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

    if retryAfter, parseErr := strconv.Atoi(spent.Headers().Get("Retry-After")); nil != parseErr || 0 >= retryAfter {
        t.Fatalf("expected the 429 to carry the budget's Retry-After, got %q", spent.Headers().Get("Retry-After"))
    }

    challenge, _ := login.post(t, "editor", "editor", nil)
    requireLoginAnswer(t, challenge, nethttp.StatusUnauthorized, "second factor required")
}

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

func (instance *secondFactorLogin) signInAndStore(t *testing.T, username string, headerList map[string]string) string {
    t.Helper()

    response, sessionInstance := instance.post(t, username, username, headerList)
    requireLoginAnswer(t, response, nethttp.StatusOK)

    if saveErr := instance.sessionManager.SaveSession(exampleTestRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("save session: %v", saveErr)
    }

    return sessionInstance.Id()
}

func TestLoginHandler_TheSignInPastTheCapEndsTheAccountsOldestSession(t *testing.T) {
    login := newSecondFactorLogin(t)

    editorSessionId := login.signInAndStore(t, "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})

    sessionIdList := make([]string, 0, repository.UserSessionCap+1)
    for range repository.UserSessionCap + 1 {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user", nil))
    }

    if nil != mustSession(login.sessionManager.Session(exampleTestRuntime(), sessionIdList[0])) {
        t.Fatalf("expected the oldest session of the account ended by the sign-in past the cap")
    }

    for _, sessionId := range sessionIdList[1:] {
        if nil == mustSession(login.sessionManager.Session(exampleTestRuntime(), sessionId)) {
            t.Fatalf("expected the account's %d newest sessions kept, %q is gone", repository.UserSessionCap, sessionId)
        }
    }

    if nil == mustSession(login.sessionManager.Session(exampleTestRuntime(), editorSessionId)) {
        t.Fatal("expected the session of another account untouched by this account's cap")
    }
}

func TestLogoutHandler_TheSignOutGivesTheSessionsPlaceBack(t *testing.T) {
    login := newSecondFactorLogin(t)

    sessionIdList := make([]string, 0, repository.UserSessionCap)
    for range repository.UserSessionCap {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user", nil))
    }

    signedOutSessionId := sessionIdList[len(sessionIdList)-1]

    logoutRequest := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/logout/", nil), login.runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("logout-test", time.Now())})
    _ = melodyhttp.SetRequestSession(logoutRequest, mustSession(login.sessionManager.Session(exampleTestRuntime(), signedOutSessionId)))

    if _, logoutErr := security.NewSessionLogoutHandler(login.sessionIndexLookup).Logout(login.runtimeInstance, logoutRequest, melodysecuritycontract.LogoutInput{}); nil != logoutErr {
        t.Fatalf("logout handler: %v", logoutErr)
    }

    login.signInAndStore(t, "user", nil)

    for _, sessionId := range sessionIdList[:len(sessionIdList)-1] {
        if nil == mustSession(login.sessionManager.Session(exampleTestRuntime(), sessionId)) {
            t.Fatalf("expected the sign-in after the sign-out to end none of the account's other sessions, %q is gone", sessionId)
        }
    }
}

func TestLoginHandler_SigningInAgainRetiresThePlaceOfTheRotatedSession(t *testing.T) {
    login := newSecondFactorLogin(t)

    otherSessionId := login.signInAndStore(t, "user", nil)

    sessionInstance := mustSession(login.sessionManager.NewSession(exampleTestRuntime()))
    for range repository.UserSessionCap + 1 {
        httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString(`{"username":"user","password":"user"}`))
        httpRequest.Header.Set("Content-Type", "application/json")

        request := melodyhttp.NewRequest(httpRequest, login.runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-again-test", time.Now())})
        _ = melodyhttp.SetRequestSession(request, sessionInstance)

        response, handlerErr := serveLoginDoor(t, login.runtimeInstance, request, login.authentication, login.sessionIndexLookup)
        if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
            t.Fatalf("expected the sign-in answered 200, got %v, %v", response, handlerErr)
        }

        sessionInstance = getSessionFromRequest(request)
        if saveErr := login.sessionManager.SaveSession(exampleTestRuntime(), sessionInstance); nil != saveErr {
            t.Fatalf("save session: %v", saveErr)
        }
    }

    if nil == mustSession(login.sessionManager.Session(exampleTestRuntime(), otherSessionId)) {
        t.Fatal("expected the account's other session kept: the sign-ins on one browser hold one place")
    }
}

func TestLoginHandler_ASessionThatEndedElsewhereGivesItsPlaceBack(t *testing.T) {
    login := newSecondFactorLogin(t)

    sessionIdList := make([]string, 0, repository.UserSessionCap)
    for range repository.UserSessionCap {
        sessionIdList = append(sessionIdList, login.signInAndStore(t, "user", nil))
    }

    if deleteErr := login.sessionStorage.Delete(exampleTestRuntime(), sessionIdList[2]); nil != deleteErr {
        t.Fatalf("unexpected error lapsing the session: %v", deleteErr)
    }

    login.clock.Advance(repository.UserSessionAdmissionGrace)

    login.signInAndStore(t, "user", nil)

    for _, sessionId := range []string{sessionIdList[0], sessionIdList[1], sessionIdList[3], sessionIdList[4]} {
        if nil == mustSession(login.sessionManager.Session(exampleTestRuntime(), sessionId)) {
            t.Fatalf("expected no live session ended while an ended one held a place, %q is gone", sessionId)
        }
    }
}

func TestLoginHandler_SignInsNotYetStoredStillMeetTheCap(t *testing.T) {
    login := newSecondFactorLogin(t)

    pendingList := make([]melodysessioncontract.Session, 0, repository.UserSessionCap+2)
    for range repository.UserSessionCap + 2 {
        response, sessionInstance := login.post(t, "user", "user", nil)
        requireLoginAnswer(t, response, nethttp.StatusOK)

        pendingList = append(pendingList, sessionInstance)
    }

    storedCount := 0
    for _, sessionInstance := range pendingList {
        if saveErr := login.sessionManager.SaveSession(exampleTestRuntime(), sessionInstance); nil == saveErr {
            storedCount++
        }
    }

    if repository.UserSessionCap != storedCount {
        t.Fatalf("expected %d of the account's sessions stored, got %d", repository.UserSessionCap, storedCount)
    }
}

type unreadableBudgetLimiter struct{}

func (instance *unreadableBudgetLimiter) Allow(runtimeInstance melodycontainercontract.Runtime, key string) (melodyhttpcontract.RateLimitDecision, error) {
    storeErr := melodyexception.NewError("redis rate limiter store failure", map[string]any{"key": key}, melodyexception.MarkInfrastructureFailure(errors.New("connection refused")))

    return melodyhttpcontract.RateLimitDecision{}, storeErr
}

func (instance *unreadableBudgetLimiter) Reset(runtimeInstance melodycontainercontract.Runtime, key string) error {
    return nil
}

func TestLoginHandler_AnswersAnUnreadableBudgetWith503RetryAfterAndOneRecord(t *testing.T) {
    journal := &loginJournalRecordingLogger{Logger: melodylogging.NewNopLogger()}
    login := newSecondFactorLoginWith(t, secondFactorLoginOptions{
        limiter: func(clockInstance melodyclockcontract.Clock) melodyhttpcontract.RateLimiter {
            return &unreadableBudgetLimiter{}
        },
        logger: journal,
    })

    response, sessionInstance := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})

    requireLoginAnswer(t, response, nethttp.StatusServiceUnavailable, "service unavailable")
    if "5" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected the firewall's Retry-After, got %q", response.Headers().Get("Retry-After"))
    }

    if 0 != len(journal.recordsNamed("rate limiter store failure")) || 1 != len(journal.recordsNamed("security login door authenticator could not verify the credential: its platform is unavailable")) || 0 != len(journal.recordsNamed("handler answered a server error")) {
        t.Fatalf("expected the outage filed once, by the door, got %v", journal.errorRecords)
    }

    if "" != sessionInstance.String(security.SessionKeySecurityUserId) || 0 != len(*login.failureList) {
        t.Fatalf("expected no sign-in and no login failure, got %q %v", sessionInstance.String(security.SessionKeySecurityUserId), *login.failureList)
    }
}

func TestLoginHandler_AnswersAnUnavailableSecondFactorStoreWith503RetryAfterAndOneRecord(t *testing.T) {
    journal := &loginJournalRecordingLogger{Logger: melodylogging.NewNopLogger()}
    login := newSecondFactorLoginWith(t, secondFactorLoginOptions{
        enrollments: twofactor.NewEnrollments(func(runtimeInstance melodycontainercontract.Runtime) (*twofactor.Store, error) {
            return nil, errors.New("the catalogue database is not reachable")
        }),
        logger: journal,
    })

    response, _ := login.post(t, "editor", "editor", nil)

    requireLoginAnswer(t, response, nethttp.StatusServiceUnavailable, "service unavailable")
    if "5" != response.Headers().Get("Retry-After") {
        t.Fatalf("expected the firewall's Retry-After, got %q", response.Headers().Get("Retry-After"))
    }

    if 1 != len(journal.recordsNamed("security login door authenticator could not verify the credential: its platform is unavailable")) || 0 != len(journal.recordsNamed("handler answered a server error")) {
        t.Fatalf("expected the unavailable store filed once by the door, got %v", journal.errorRecords)
    }

    if 0 != len(*login.failureList) {
        t.Fatalf("expected an unavailable store not announced as a failed sign-in, got %v", *login.failureList)
    }
}

type strangerTokenAuthenticator struct{}

func (instance *strangerTokenAuthenticator) Authenticate(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request) (melodysecuritycontract.Token, error) {
    return melodysecurity.NewAuthenticatedToken("user-9", []string{"ROLE_USER"}, nil), nil
}

func TestLoginHandler_FilesATokenForAnAccountTheSignInNeverLoaded(t *testing.T) {
    journal := &loginJournalRecordingLogger{Logger: melodylogging.NewNopLogger()}
    login := newSecondFactorLoginWith(t, secondFactorLoginOptions{logger: journal})
    login.authentication = &strangerTokenAuthenticator{}

    response, _ := login.post(t, "editor", "editor", nil)

    requireLoginAnswer(t, response, nethttp.StatusInternalServerError, "authentication failed")

    records := journal.recordsNamed("handler answered a server error")
    if 1 != len(records) {
        t.Fatalf("expected the mismatch filed once, got %v", journal.errorRecords)
    }

    if false == strings.Contains(fmt.Sprint(records[0].context["causeContextChain"]), "tokenUser:user-9") {
        t.Fatalf("expected the record to name the token's user, got %v", records[0].context)
    }
}

func loginJsonBody(t *testing.T, username string, password string) *bytes.Buffer {
    t.Helper()

    encoded, encodeErr := json.Marshal(map[string]string{"username": username, "password": password})
    if nil != encodeErr {
        t.Fatalf("encode the sign-in body: %v", encodeErr)
    }

    return bytes.NewBuffer(encoded)
}

func TestLoginHandler_RefusesAFormBodyWithoutAnnouncingAFailedSignIn(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", bytes.NewBufferString("username=nobody&password=wrong"))
    httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-form-refused-test", time.Now())})
    publishSignInSession(t, request)

    response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
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

func TestLoginHandler_RotatesTheSessionIdAndRetiresThePreLoginOne(t *testing.T) {
    login := newSecondFactorLogin(t)

    preLogin := mustSession(login.sessionManager.NewSession(exampleTestRuntime()))
    preLogin.Set("example.cart", "three items")
    if saveErr := login.sessionManager.SaveSession(exampleTestRuntime(), preLogin); nil != saveErr {
        t.Fatalf("save the pre-login session: %v", saveErr)
    }
    preLoginId := preLogin.Id()

    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, "user", "user"))
    httpRequest.Header.Set("Content-Type", "application/json")

    request := melodyhttp.NewRequest(httpRequest, login.runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-rotation-test", time.Now())})
    _ = melodyhttp.SetRequestSession(request, mustSession(login.sessionManager.Session(exampleTestRuntime(), preLoginId)))

    response, handlerErr := serveLoginDoor(t, login.runtimeInstance, request, login.authentication, login.sessionIndexLookup)
    if nil != handlerErr || nil == response || nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected the sign-in answered 200, got %v, %v", response, handlerErr)
    }

    rotated := getSessionFromRequest(request)
    if nil == rotated || preLoginId == rotated.Id() {
        t.Fatalf("expected the session id rotated away from %q, got %v", preLoginId, rotated)
    }

    if nil != mustSession(login.sessionManager.Session(exampleTestRuntime(), preLoginId)) {
        t.Fatalf("expected the pre-login session %q retired from the storage", preLoginId)
    }

    if "" == rotated.String(security.SessionKeySecurityUserId) || "three items" != rotated.String("example.cart") {
        t.Fatalf("expected the identity and the pre-login values on the rotated session, got %v", rotated.All())
    }
}

func TestLoginHandler_AnswersABodyPastTheLimit413(t *testing.T) {
    runtimeInstance, failureList := loginRuntimeRefusingEveryCredential(t)

    recorder := httptest.NewRecorder()
    httpRequest := httptest.NewRequest(nethttp.MethodPost, "/login", loginJsonBody(t, "nobody", "wrong"))
    httpRequest.Header.Set("Content-Type", "application/json")
    httpRequest.Body = nethttp.MaxBytesReader(recorder, httpRequest.Body, 8)

    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("login-body-limit-test", time.Now())})
    publishSignInSession(t, request)

    response, handlerErr := serveLoginDoor(t, runtimeInstance, request, passwordOnlyLogin(), nil)
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

func TestLandingPageName_SendsEachRoleToAPageItOpens(t *testing.T) {
    expectations := []struct {
        roleList []string
        expected string
    }{
        {[]string{entity.RoleUser}, route.CataloguePageName},
        {[]string{entity.RoleEditor}, route.ProductsListPageName},
        {[]string{entity.RoleAdmin}, route.ProductsListPageName},
        {[]string{}, route.CataloguePageName},
    }

    for _, expectation := range expectations {
        if actual := security.LandingPageName(expectation.roleList); expectation.expected != actual {
            t.Fatalf("expected %v to land on %s, got %s", expectation.roleList, expectation.expected, actual)
        }
    }
}

func landingPageRouter() *melodyhttp.Router {
    router := melodyhttp.NewRouter()
    landingPage := func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return nil, nil
    }

    router.Handle(route.ProductsListPagePattern, landingPage, melodyhttp.RouteOptions{Name: route.ProductsListPageName, Methods: []string{"GET"}})
    router.Handle(route.CataloguePagePattern, landingPage, melodyhttp.RouteOptions{Name: route.CataloguePageName, Methods: []string{"GET"}})

    return router
}

func TestLoginHandler_AnswersTheLandingPageTheRoleOpens(t *testing.T) {
    login := newSecondFactorLogin(t)

    userResponse, _ := login.post(t, "user", "user", nil)
    requireLoginAnswer(t, userResponse, nethttp.StatusOK, `"redirectUrl":"/catalogue"`)

    editorResponse, _ := login.post(t, "editor", "editor", map[string]string{melodysecurity.DefaultTotpCodeHeaderName: login.currentCode(t)})
    requireLoginAnswer(t, editorResponse, nethttp.StatusOK, `"redirectUrl":"/products"`)
}

func mustSession(sessionInstance melodysessioncontract.Session, sessionErr error) melodysessioncontract.Session {
    if nil != sessionErr {
        panic(sessionErr)
    }

    return sessionInstance
}

func exampleTestRuntime() melodycontainercontract.Runtime {
    return melodyruntime.NewRun(context.Background(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}
