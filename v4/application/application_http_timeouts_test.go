package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func newLimitConfiguration(t *testing.T, values map[string]string) configcontract.Configuration {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return configuration
}

func TestApplyHttpServerTimeoutsDefaults(t *testing.T) {
    limits, limitsErr := resolveHttpServerLimits(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{})
    if nil != limitsErr {
        t.Fatalf("unexpected refusal: %v", limitsErr)
    }

    server := &nethttp.Server{}
    applyHttpServerTimeouts(server, limits)

    if 15*time.Second != server.ReadTimeout {
        t.Fatalf("expected default ReadTimeout 15s, got %v", server.ReadTimeout)
    }
    if 5*time.Second != server.ReadHeaderTimeout {
        t.Fatalf("expected default ReadHeaderTimeout 5s, got %v", server.ReadHeaderTimeout)
    }
    if 30*time.Second != server.WriteTimeout {
        t.Fatalf("expected default WriteTimeout 30s, got %v", server.WriteTimeout)
    }
    if 60*time.Second != server.IdleTimeout {
        t.Fatalf("expected default IdleTimeout 60s, got %v", server.IdleTimeout)
    }
    if 1<<20 != server.MaxHeaderBytes {
        t.Fatalf("expected default MaxHeaderBytes 1 MiB, got %v", server.MaxHeaderBytes)
    }
}

func TestResolveHttpServerLimits_TheKeyWinsOverTheFieldAndTheFieldOverTheDefault(t *testing.T) {
    applicationConfig := ApplicationConfig{
        ReadTimeout:       bound.Of(10 * time.Second),
        ReadHeaderTimeout: bound.Of(2 * time.Second),
        WriteTimeout:      bound.Of(20 * time.Second),
        IdleTimeout:       bound.Of(40 * time.Second),
        MaxHeaderBytes:    bound.Of(8192),
    }

    fromField, fromFieldErr := resolveHttpServerLimits(newLimitConfiguration(t, map[string]string{}), applicationConfig)
    if nil != fromFieldErr {
        t.Fatalf("unexpected refusal: %v", fromFieldErr)
    }

    if (httpServerLimits{readTimeout: 10 * time.Second, readHeaderTimeout: 2 * time.Second, writeTimeout: 20 * time.Second, idleTimeout: 40 * time.Second, maxHeaderBytes: 8192}) != fromField {
        t.Fatalf("expected the fields to win over the defaults, got %+v", fromField)
    }

    fromKey, fromKeyErr := resolveHttpServerLimits(
        newLimitConfiguration(t, map[string]string{
            config.HttpReadTimeoutKey:       "20s",
            config.HttpReadHeaderTimeoutKey: "3s",
            config.HttpWriteTimeoutKey:      "35s",
            config.HttpIdleTimeoutKey:       "70s",
            config.HttpMaxHeaderBytesKey:    "4096",
        }),
        applicationConfig,
    )
    if nil != fromKeyErr {
        t.Fatalf("unexpected refusal: %v", fromKeyErr)
    }

    if (httpServerLimits{readTimeout: 20 * time.Second, readHeaderTimeout: 3 * time.Second, writeTimeout: 35 * time.Second, idleTimeout: 70 * time.Second, maxHeaderBytes: 4096}) != fromKey {
        t.Fatalf("expected the keys to win over the fields, got %+v", fromKey)
    }
}

func TestResolveHttpServerLimits_RefusesAFieldItsRuleDoesNotAdmit(t *testing.T) {
    configuration := newLimitConfiguration(t, map[string]string{})

    for _, applicationConfig := range []ApplicationConfig{
        {ReadTimeout: bound.Of(time.Duration(0))},
        {ReadHeaderTimeout: bound.UnlimitedDuration()},
        {WriteTimeout: bound.Of(-time.Second)},
        {IdleTimeout: bound.UnlimitedDuration()},
        {MaxHeaderBytes: bound.Of(4095)},
        {ReadTimeout: bound.UnlimitedDuration()},
        {WriteTimeout: bound.UnlimitedDuration()},
        {MaxHeaderBytes: bound.UnlimitedCount()},
    } {
        if _, limitsErr := resolveHttpServerLimits(configuration, applicationConfig); nil == limitsErr {
            t.Fatalf("expected %+v refused", applicationConfig)
        }
    }
}

func TestResolveTeardownTimeout_FollowsTheSamePrecedenceAndAdmitsUnlimited(t *testing.T) {
    byDefault, byDefaultErr := resolveTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{})
    if nil != byDefaultErr || 10*time.Second != byDefault.Value {
        t.Fatalf("expected the 10s default, got %+v (%v)", byDefault, byDefaultErr)
    }

    fromField, fromFieldErr := resolveTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{TeardownTimeout: bound.UnlimitedDuration()})
    if nil != fromFieldErr || false == fromField.Unlimited {
        t.Fatalf("expected the field's unlimited, got %+v (%v)", fromField, fromFieldErr)
    }

    fromKey, fromKeyErr := resolveTeardownTimeout(newLimitConfiguration(t, map[string]string{config.TeardownTimeoutKey: "25s"}), ApplicationConfig{TeardownTimeout: bound.UnlimitedDuration()})
    if nil != fromKeyErr || 25*time.Second != fromKey.Value {
        t.Fatalf("expected the key to win, got %+v (%v)", fromKey, fromKeyErr)
    }

    if _, refusedErr := resolveTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{TeardownTimeout: bound.Of(time.Duration(0))}); nil == refusedErr {
        t.Fatalf("expected a zero teardown field refused")
    }
}

func TestTeardownBudget_KeepsUnlimitedAndTheExitPathLeavesItOut(t *testing.T) {
    applicationInstance := &Application{
        configuration:     newLimitConfiguration(t, map[string]string{}),
        applicationConfig: ApplicationConfig{TeardownTimeout: bound.UnlimitedDuration()},
    }
    if false == applicationInstance.teardownBudget().IsUnlimited() {
        t.Fatalf("expected unlimited kept, got %v", applicationInstance.teardownBudget())
    }
    if false == applicationInstance.exitConfig().BeforeExitBudget.IsAbsent() {
        t.Fatalf("expected the exit path to leave an unlimited budget out, got %v", applicationInstance.exitConfig().BeforeExitBudget)
    }

    applicationInstance.applicationConfig = ApplicationConfig{TeardownTimeout: bound.Of(20 * time.Second)}
    if budget, isValue := applicationInstance.teardownBudget().Value(); false == isValue || 20*time.Second != budget {
        t.Fatalf("expected the field's budget, got %v", applicationInstance.teardownBudget())
    }
    if budget, isValue := applicationInstance.exitConfig().BeforeExitBudget.Value(); false == isValue || 20*time.Second != budget {
        t.Fatalf("expected the exit path to run under the field's budget, got %v", applicationInstance.exitConfig().BeforeExitBudget)
    }

    applicationInstance.applicationConfig = ApplicationConfig{TeardownTimeout: bound.Of(time.Duration(0))}
    if budget, isValue := applicationInstance.teardownBudget().Value(); false == isValue || config.DefaultTeardownTimeout != budget {
        t.Fatalf("expected a refused budget to fall back to the default, got %v", applicationInstance.teardownBudget())
    }
}

type errorLogCapturingLogger struct {
    loggingcontract.Logger

    warnings []loggingcontract.Context
    messages []string
}

func (instance *errorLogCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
    instance.warnings = append(instance.warnings, context)
}

func TestApplyHttpServerErrorLog_WiresTheApplicationLogger(t *testing.T) {
    server := &nethttp.Server{}
    capture := &errorLogCapturingLogger{Logger: logging.NewNopLogger()}

    applyHttpServerErrorLog(server, capture)

    if nil == server.ErrorLog {
        t.Fatalf("expected net/http to report through the application logger")
    }

    server.ErrorLog.Printf("http: TLS handshake error from 10.0.0.1:52000: EOF")

    if 1 != len(capture.messages) {
        t.Fatalf("expected one record, got %v", capture.messages)
    }

    if "http server error" != capture.messages[0] {
        t.Fatalf("expected the groupable message, got %q", capture.messages[0])
    }

    if "http: TLS handshake error from 10.0.0.1:52000: EOF" != capture.warnings[0]["line"] {
        t.Fatalf("expected the line to travel in the context, got %v", capture.warnings[0]["line"])
    }
}

func TestResolveContainerCreationWaiters_FollowsTheSamePrecedenceAndAdmitsUnlimited(t *testing.T) {
    byDefault, byDefaultErr := resolveContainerCreationWaiters(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{})
    if nil != byDefaultErr || 1024 != byDefault.Value {
        t.Fatalf("expected the 1,024 default, got %+v (%v)", byDefault, byDefaultErr)
    }

    fromField, fromFieldErr := resolveContainerCreationWaiters(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{ContainerCreationWaiters: bound.UnlimitedCount()})
    if nil != fromFieldErr || false == fromField.Unlimited {
        t.Fatalf("expected the field's unlimited, got %+v (%v)", fromField, fromFieldErr)
    }

    fromKey, fromKeyErr := resolveContainerCreationWaiters(newLimitConfiguration(t, map[string]string{config.ContainerCreationWaitersKey: "64"}), ApplicationConfig{ContainerCreationWaiters: bound.UnlimitedCount()})
    if nil != fromKeyErr || 64 != fromKey.Value {
        t.Fatalf("expected the key to win, got %+v (%v)", fromKey, fromKeyErr)
    }

    if _, refusedErr := resolveContainerCreationWaiters(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{ContainerCreationWaiters: bound.Of(0)}); nil == refusedErr {
        t.Fatalf("expected a zero creation waiters field refused")
    }
}

func TestResolveContainerTeardownSequential_TheKeyWinsOverTheField(t *testing.T) {
    if true == resolveContainerTeardownSequential(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{}) {
        t.Fatalf("expected the waves by default")
    }

    if false == resolveContainerTeardownSequential(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{ContainerTeardownSequential: true}) {
        t.Fatalf("expected the field to choose the sequential teardown when the key says nothing")
    }

    if true == resolveContainerTeardownSequential(newLimitConfiguration(t, map[string]string{config.ContainerTeardownSequentialKey: "false"}), ApplicationConfig{ContainerTeardownSequential: true}) {
        t.Fatalf("expected the key to win over the field")
    }

    if false == resolveContainerTeardownSequential(newLimitConfiguration(t, map[string]string{config.ContainerTeardownSequentialKey: "true"}), ApplicationConfig{}) {
        t.Fatalf("expected the key to choose the sequential teardown")
    }
}

func TestResolveScopeTeardownTimeout_FollowsTheSamePrecedenceAndAdmitsUnlimited(t *testing.T) {
    byDefault, byDefaultErr := resolveScopeTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{})
    if nil != byDefaultErr || container.DefaultScopeTeardownTimeout != byDefault.Value {
        t.Fatalf("expected the default, got %+v (%v)", byDefault, byDefaultErr)
    }

    fromField, fromFieldErr := resolveScopeTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{ScopeTeardownTimeout: bound.UnlimitedDuration()})
    if nil != fromFieldErr || false == fromField.Unlimited {
        t.Fatalf("expected the field's unlimited, got %+v (%v)", fromField, fromFieldErr)
    }

    fromKey, fromKeyErr := resolveScopeTeardownTimeout(newLimitConfiguration(t, map[string]string{config.ScopeTeardownTimeoutKey: "7s"}), ApplicationConfig{ScopeTeardownTimeout: bound.UnlimitedDuration()})
    if nil != fromKeyErr || 7*time.Second != fromKey.Value {
        t.Fatalf("expected the key to win, got %+v (%v)", fromKey, fromKeyErr)
    }

    if _, refusedErr := resolveScopeTeardownTimeout(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{ScopeTeardownTimeout: bound.Of(time.Duration(0))}); nil == refusedErr {
        t.Fatalf("expected a zero scope teardown timeout field refused")
    }
}

func TestRefuseScopeTeardownOutlastingHttpShutdown_AdmitsOnlyATermStrictlyUnderTheWait(t *testing.T) {
    if refusedErr := refuseScopeTeardownOutlastingHttpShutdown(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{}); nil != refusedErr {
        t.Fatalf("expected the two defaults admitted, got %v", refusedErr)
    }

    admitted := newLimitConfiguration(t, map[string]string{config.HttpShutdownTimeoutKey: "2s", config.ScopeTeardownTimeoutKey: "1999ms"})
    if refusedErr := refuseScopeTeardownOutlastingHttpShutdown(admitted, ApplicationConfig{}); nil != refusedErr {
        t.Fatalf("expected a term just under the wait admitted, got %v", refusedErr)
    }

    refusedCases := map[string]struct {
        values            map[string]string
        applicationConfig ApplicationConfig
    }{
        "equal":                 {values: map[string]string{config.HttpShutdownTimeoutKey: "2s", config.ScopeTeardownTimeoutKey: "2s"}},
        "above":                 {values: map[string]string{config.HttpShutdownTimeoutKey: "2s", config.ScopeTeardownTimeoutKey: "3s"}},
        "the default above":     {values: map[string]string{config.HttpShutdownTimeoutKey: "2s"}},
        "the default equal":     {values: map[string]string{config.HttpShutdownTimeoutKey: "3s"}},
        "unlimited by the key":  {values: map[string]string{config.ScopeTeardownTimeoutKey: "unlimited"}},
        "unlimited by the field": {values: map[string]string{}, applicationConfig: ApplicationConfig{ScopeTeardownTimeout: bound.UnlimitedDuration()}},
    }

    for name, refusedCase := range refusedCases {
        refusedErr := refuseScopeTeardownOutlastingHttpShutdown(newLimitConfiguration(t, refusedCase.values), refusedCase.applicationConfig)
        if nil == refusedErr || "the scope teardown timeout must stay under the http shutdown timeout" != refusedErr.Error() {
            t.Fatalf("%s: expected the refusal, got %v", name, refusedErr)
        }

        refusalContext := exception.LogContext(refusedErr, exception.LogContextConfig{})
        if config.ScopeTeardownTimeoutKey != refusalContext["scopeTeardownKey"] || config.HttpShutdownTimeoutKey != refusalContext["httpShutdownKey"] {
            t.Fatalf("%s: expected the refusal to name both keys, got %v", name, refusalContext)
        }
    }
}

func TestResolveUnavailableRetryAfter_TheKeyWinsOverTheFieldOverTheDefault(t *testing.T) {
    byDefault, byDefaultErr := resolveUnavailableRetryAfter(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{})
    if nil != byDefaultErr || config.DefaultUnavailableRetryAfter != byDefault {
        t.Fatalf("expected the default, got %s (%v)", byDefault, byDefaultErr)
    }

    fromField, fromFieldErr := resolveUnavailableRetryAfter(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{UnavailableRetryAfter: bound.Of(9 * time.Second)})
    if nil != fromFieldErr || 9*time.Second != fromField {
        t.Fatalf("expected the field, got %s (%v)", fromField, fromFieldErr)
    }

    fromKey, fromKeyErr := resolveUnavailableRetryAfter(newLimitConfiguration(t, map[string]string{config.HttpUnavailableRetryAfterKey: "7s"}), ApplicationConfig{UnavailableRetryAfter: bound.Of(9 * time.Second)})
    if nil != fromKeyErr || 7*time.Second != fromKey {
        t.Fatalf("expected the key to win, got %s (%v)", fromKey, fromKeyErr)
    }

    for name, limit := range map[string]bound.Duration{
        "500ms":     bound.Of(500 * time.Millisecond),
        "1500ms":    bound.Of(1500 * time.Millisecond),
        "2h":        bound.Of(2 * time.Hour),
        "unlimited": bound.UnlimitedDuration(),
    } {
        _, refusedErr := resolveUnavailableRetryAfter(newLimitConfiguration(t, map[string]string{}), ApplicationConfig{UnavailableRetryAfter: limit})
        if nil == refusedErr || "ApplicationConfig.UnavailableRetryAfter" != exception.LogContext(refusedErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("%s: expected a refusal naming the field, got %v", name, refusedErr)
        }
    }
}

func TestNewApplication_TheUnavailableRetryAfterFieldReachesTheHttpKernel(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), UnavailableRetryAfter: bound.Of(13 * time.Second)})
    serviceContainer := applicationInstance.kernel.ServiceContainer()
    defer func() {
        _ = serviceContainer.Close()
    }()

    applicationInstance.Boot()

    applicationInstance.kernel.HttpRouter().Handle(
        nethttp.MethodGet,
        "/unavailable",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.MarkInfrastructureFailure(errors.New("cache is down"))
        },
    )

    recorder := httptest.NewRecorder()
    applicationInstance.kernel.HttpKernel().ServeHttp(serviceContainer).ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/unavailable", nil))

    if nethttp.StatusServiceUnavailable != recorder.Code || "13" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with the field's Retry-After 13, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}
