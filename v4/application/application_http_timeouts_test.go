package application

import (
    nethttp "net/http"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

func TestTeardownTimeout_HandsUnlimitedToTheExitPathAsZero(t *testing.T) {
    applicationInstance := &Application{
        configuration:     newLimitConfiguration(t, map[string]string{}),
        applicationConfig: ApplicationConfig{TeardownTimeout: bound.UnlimitedDuration()},
    }
    if 0 != applicationInstance.teardownTimeout() {
        t.Fatalf("expected unlimited to read as zero, got %v", applicationInstance.teardownTimeout())
    }

    applicationInstance.applicationConfig = ApplicationConfig{TeardownTimeout: bound.Of(20 * time.Second)}
    if 20*time.Second != applicationInstance.teardownTimeout() {
        t.Fatalf("expected the field's budget, got %v", applicationInstance.teardownTimeout())
    }

    applicationInstance.applicationConfig = ApplicationConfig{TeardownTimeout: bound.Of(time.Duration(0))}
    if config.DefaultTeardownTimeout != applicationInstance.teardownTimeout() {
        t.Fatalf("expected a refused budget to fall back to the default, got %v", applicationInstance.teardownTimeout())
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
