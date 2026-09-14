package handler

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/security"
    "github.com/precision-soft/melody/v3/.example/service"
    "github.com/precision-soft/melody/v3/.example/entity"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
)

/* the cause the login door can really reach names internals: a repository error carries the schema and table, and the endpoint is unauthenticated */
const authenticationCauseSecret = "dial tcp 10.0.0.7:3306: connect: connection refused"

type stubAuthenticationEnvironmentSource struct {
    values map[string]string
}

func (instance *stubAuthenticationEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

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

    response, handlerErr := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
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


func TestLoginDecoderDiagnosticRemainsAvailableInDebug(t *testing.T) {
    runtimeInstance := loginRuntimeForEnvironment(t, melodyconfig.EnvDevelopment)
    incoming := httptest.NewRequest(nethttp.MethodPost, "/login", strings.NewReader("{"))
    incoming.Header.Set("Content-Type", "application/json")
    request := melodyhttp.NewRequest(incoming, nil, runtimeInstance, nil)
    response, err := LoginHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != err { t.Fatal(err) }
    body, err := io.ReadAll(response.BodyReader())
    if nil != err || false == strings.Contains(string(body), "unexpected EOF") { t.Fatalf("decoder diagnosis disappeared: %s err=%v", body, err) }
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
            containerInstance := melodycontainer.NewContainer()
            if err := melodycontainer.Register[*service.UserService](containerInstance, service.ServiceUserService,
                func(resolver melodycontainercontract.Resolver) (*service.UserService, error) {
                    return service.NewUserService(repositoryInstance, nil, nil), nil
                }); nil != err {
                t.Fatal(err)
            }
            runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
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
