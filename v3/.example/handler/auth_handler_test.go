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
    "github.com/precision-soft/melody/v3/.example/service"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* the cause the login door can really reach names internals: a cache backend refusal carries the store address, a repository error carries the schema and table, and the endpoint is unauthenticated */
const authenticationCauseSecret = "dial tcp 10.0.0.7:6379: connect: connection refused"

type stubAuthenticationEnvironmentSource struct {
    values map[string]string
}

func (instance *stubAuthenticationEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* the cache refuses on the read the authentication performs, which is how a broken backend reaches AuthenticateByUsernameAndPassword on this door */
type refusingCache struct{}

func (instance *refusingCache) Get(key string) (any, bool, error) {
    return nil, false, errors.New(authenticationCauseSecret)
}

func (instance *refusingCache) Set(key string, value any, ttl time.Duration) error {
    return nil
}

func (instance *refusingCache) Delete(key string) error {
    return nil
}

func (instance *refusingCache) Has(key string) (bool, error) {
    return false, nil
}

func (instance *refusingCache) Clear() error {
    return nil
}

func (instance *refusingCache) Many(keys []string) (map[string]any, error) {
    return map[string]any{}, nil
}

func (instance *refusingCache) SetMultiple(items map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *refusingCache) DeleteMultiple(keys []string) error {
    return nil
}

func (instance *refusingCache) Increment(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *refusingCache) Decrement(key string, delta int64) (int64, error) {
    return 0, nil
}

func (instance *refusingCache) Close() error {
    return nil
}

var _ melodycachecontract.Cache = (*refusingCache)(nil)

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
            var userRepository repository.UserRepository

            return service.NewUserService(userRepository, &refusingCache{}, nil), nil
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

/* the login door is unauthenticated, so an authentication failure must answer a public message and nothing else: the errors list is written into the response with no debug gate at all, and the causes this call can really reach — a cache dial refusal, a driver error naming the schema and the host, a deserialization failure — would be handed to anonymous callers verbatim. ApiErrorWithErr is the door that keeps the cause behind the debug gates instead. */
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
