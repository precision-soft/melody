package pgsql

import (
    "context"
    "errors"
    "math"
    "net"
    "os"
    "reflect"
    "testing"
    "time"

    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/uptrace/bun/driver/pgdriver"
)

/* @info test stubs: the root-line provider resolves its connection parameters from a container resolver, so the tests stub the resolver and the configuration service */

type stubParameter struct {
    value string
}

func (instance *stubParameter) EnvironmentKey() string {
    return ""
}

func (instance *stubParameter) EnvironmentValue() any {
    return nil
}

func (instance *stubParameter) Value() any {
    return instance.value
}

func (instance *stubParameter) IsDefault() bool {
    return false
}

func (instance *stubParameter) String() string {
    return instance.value
}

func (instance *stubParameter) MustString() string {
    return instance.value
}

func (instance *stubParameter) Bool() (bool, error) {
    return false, errors.New("not implemented")
}

func (instance *stubParameter) Int() (int, error) {
    return 0, errors.New("not implemented")
}

func (instance *stubParameter) IsSecret() bool {
    return false
}

func (instance *stubParameter) Float() (float64, error) {
    return 0, errors.New("not implemented")
}

func (instance *stubParameter) Duration() (time.Duration, error) {
    return 0, errors.New("not implemented")
}

type stubConfiguration struct {
    parameters map[string]string
}

func (instance *stubConfiguration) Get(name string) configcontract.Parameter {
    return &stubParameter{value: instance.parameters[name]}
}

func (instance *stubConfiguration) MustGet(name string) configcontract.Parameter {
    value, found := instance.parameters[name]
    if false == found {
        panic("parameter not found: " + name)
    }

    return &stubParameter{value: value}
}

func (instance *stubConfiguration) RegisterRuntime(name string, value any) {
}

func (instance *stubConfiguration) RegisterRuntimeSecret(name string, value any) {
}

func (instance *stubConfiguration) MarkSecret(name string) bool {
    return false
}

func (instance *stubConfiguration) Resolve() error {
    return nil
}

func (instance *stubConfiguration) Cli() configcontract.CliConfiguration {
    return nil
}

func (instance *stubConfiguration) Kernel() configcontract.KernelConfiguration {
    return nil
}

func (instance *stubConfiguration) Http() configcontract.HttpConfiguration {
    return nil
}

func (instance *stubConfiguration) Names() []string {
    names := make([]string, 0, len(instance.parameters))
    for name := range instance.parameters {
        names = append(names, name)
    }

    return names
}

type stubResolver struct {
    configuration configcontract.Configuration
}

func (instance *stubResolver) Get(serviceName string) (any, error) {
    if config.ServiceConfig == serviceName {
        return instance.configuration, nil
    }

    return nil, errors.New("service not registered: " + serviceName)
}

func (instance *stubResolver) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        panic(getErr)
    }

    return value
}

func (instance *stubResolver) GetByType(targetType reflect.Type) (any, error) {
    return nil, errors.New("not implemented")
}

func (instance *stubResolver) MustGetByType(targetType reflect.Type) any {
    panic("not implemented")
}

func (instance *stubResolver) Has(serviceName string) bool {
    return config.ServiceConfig == serviceName
}

func (instance *stubResolver) HasType(targetType reflect.Type) bool {
    return false
}

func newStubResolver(host string, port string, database string, user string, password string) *stubResolver {
    return &stubResolver{
        configuration: &stubConfiguration{
            parameters: map[string]string{
                "database.host":     host,
                "database.port":     port,
                "database.name":     database,
                "database.user":     user,
                "database.password": password,
            },
        },
    }
}

func newTestProvider(providerOptions ...ProviderOption) *Provider {
    return NewProvider(
        "database.host",
        "database.port",
        "database.name",
        "database.user",
        "database.password",
        providerOptions...,
    )
}

/* @info provider construction and option resolution */

func TestNewProviderStoresParameterNames(t *testing.T) {
    provider := newTestProvider()

    if "database.host" != provider.hostParameterName {
        t.Fatalf("expected host parameter name to be stored, got %q", provider.hostParameterName)
    }
    if "database.port" != provider.portParameterName {
        t.Fatalf("expected port parameter name to be stored, got %q", provider.portParameterName)
    }
    if "database.name" != provider.databaseParameterName {
        t.Fatalf("expected database parameter name to be stored, got %q", provider.databaseParameterName)
    }
    if "database.user" != provider.userParameterName {
        t.Fatalf("expected user parameter name to be stored, got %q", provider.userParameterName)
    }
    if "database.password" != provider.passwordParameterName {
        t.Fatalf("expected password parameter name to be stored, got %q", provider.passwordParameterName)
    }

    if nil != provider.poolConfig || nil != provider.timeoutConfig || nil != provider.retryConfig {
        t.Fatalf("expected pool/timeout/retry configs to default to nil")
    }
}

func TestNewProviderWithConfigStoresConfigs(t *testing.T) {
    poolConfig := DefaultPoolConfig()
    timeoutConfig := DefaultTimeoutConfig()
    retryConfig := DefaultRetryConfig()

    provider := NewProviderWithConfig(
        "database.host",
        "database.port",
        "database.name",
        "database.user",
        "database.password",
        poolConfig,
        timeoutConfig,
        retryConfig,
    )

    if poolConfig != provider.poolConfig {
        t.Fatalf("expected the pool config to be stored")
    }
    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("expected the timeout config to be stored")
    }
    if retryConfig != provider.retryConfig {
        t.Fatalf("expected the retry config to be stored")
    }
}

func TestProviderBuilderMethodsSetConfigs(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)
    timeoutConfig := NewTimeoutConfig(time.Second)
    retryConfig := NewRetryConfig(4, time.Millisecond, time.Second, 3.0)

    provider := newTestProvider().
        WithPoolConfig(poolConfig).
        WithTimeoutConfig(timeoutConfig).
        WithRetryConfig(retryConfig)

    if poolConfig != provider.poolConfig {
        t.Fatalf("expected WithPoolConfig to set the pool config")
    }
    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("expected WithTimeoutConfig to set the timeout config")
    }
    if retryConfig != provider.retryConfig {
        t.Fatalf("expected WithRetryConfig to set the retry config")
    }
}

/* @info Open resolves the configuration parameters and aborts on a post-build hook error before dialing */

func TestProviderOpenResolvesConfigParametersAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConnector *pgdriver.Connector
    provider := newTestProvider(
        WithInsecure(true),
        WithPostBuildHook(func(ctx context.Context, resolver containercontract.Resolver, connector *pgdriver.Connector) error {
            seenConnector = connector

            return hookErr
        }),
    )

    resolver := newStubResolver("db.internal", "5432", "melody", "melody_user", "melody_password")

    database, openErr := provider.Open(resolver)
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle when the post-build hook fails")
    }

    if false == errors.Is(openErr, hookErr) {
        t.Fatalf("expected the hook error to be wrapped, got: %v", openErr)
    }

    if nil == seenConnector {
        t.Fatalf("expected the post-build hook to receive the connector")
    }

    connectorConfig := seenConnector.Config()
    if "db.internal:5432" != connectorConfig.Addr {
        t.Fatalf("expected the resolved address db.internal:5432, got %q", connectorConfig.Addr)
    }
    if "melody" != connectorConfig.Database {
        t.Fatalf("expected the resolved database melody, got %q", connectorConfig.Database)
    }
    if "melody_user" != connectorConfig.User {
        t.Fatalf("expected the resolved user melody_user, got %q", connectorConfig.User)
    }
    if "melody_password" != connectorConfig.Password {
        t.Fatalf("expected the resolved password to be passed to the driver, got %q", connectorConfig.Password)
    }
}

/* @info openWithRetry must fall back to the emergency logger when the resolver has no logger service instead of panicking on the warning path */

func TestProviderOpenWithRetryAndNoLoggerServiceDoesNotPanic(t *testing.T) {
    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(100 * time.Millisecond)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0))

    resolver := newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    database, openErr := provider.Open(resolver)
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle for an unreachable host")
    }

    if nil == openErr {
        t.Fatalf("expected a connection error for an unreachable host")
    }
}

func TestProviderOpenWithZeroConnectTimeoutConnects(t *testing.T) {
    host := os.Getenv("PGSQL_HOST")
    if "" == host {
        t.Skip("PGSQL_HOST not set; skipping pgsql provider integration test")
    }

    resolver := newStubResolver(
        host,
        os.Getenv("PGSQL_PORT"),
        os.Getenv("PGSQL_DATABASE"),
        os.Getenv("PGSQL_USER"),
        os.Getenv("PGSQL_PASSWORD"),
    )

    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(0))

    database, openErr := provider.Open(resolver)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}

/* @info retry backoff and transient-error classification */

/* @info test errors for the transient classification */

type stubTimeoutError struct {
    message string
}

func (instance *stubTimeoutError) Error() string {
    return instance.message
}

func (instance *stubTimeoutError) Timeout() bool {
    return true
}

func (instance *stubTimeoutError) Temporary() bool {
    return false
}

type stubTemporaryError struct {
    message string
}

func (instance *stubTemporaryError) Error() string {
    return instance.message
}

func (instance *stubTemporaryError) Timeout() bool {
    return false
}

func (instance *stubTemporaryError) Temporary() bool {
    return true
}

type wrappedError struct {
    message string
    cause   error
}

func (instance *wrappedError) Error() string {
    return instance.message
}

func (instance *wrappedError) Unwrap() error {
    return instance.cause
}

var _ net.Error = (*stubTimeoutError)(nil)

var _ net.Error = (*stubTemporaryError)(nil)

func TestComputeBackoffDelayGrowsExponentiallyAndClampsAtMaxDelay(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(3, 100*time.Millisecond, 250*time.Millisecond, 2.0))

    if 100*time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected the first attempt to use the initial delay, got %s", provider.computeBackoffDelay(1))
    }

    if 200*time.Millisecond != provider.computeBackoffDelay(2) {
        t.Fatalf("expected the second attempt to double the initial delay, got %s", provider.computeBackoffDelay(2))
    }

    if 250*time.Millisecond != provider.computeBackoffDelay(3) {
        t.Fatalf("expected the third attempt to clamp at the max delay, got %s", provider.computeBackoffDelay(3))
    }
}

func TestComputeBackoffDelayZeroValuesFallBackToDefaults(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(&RetryConfig{})

    if 500*time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected the default initial delay of 500ms, got %s", provider.computeBackoffDelay(1))
    }

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected the default multiplier of 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the default max delay clamp of 5s, got %s", provider.computeBackoffDelay(10))
    }
}

func TestComputeBackoffDelayDegenerateValuesFallBackToDefaults(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(3, -time.Second, -time.Second, 0.5))

    if 500*time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected a negative initial delay to fall back to the default 500ms, got %s", provider.computeBackoffDelay(1))
    }

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a 0.5 multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected a negative max delay to fall back to the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}

func TestIsTransientErrorClassification(t *testing.T) {
    provider := newTestProvider()

    testCases := []struct {
        name      string
        inputErr  error
        transient bool
    }{
        {
            name:      "nil error is not transient",
            inputErr:  nil,
            transient: false,
        },
        {
            name:      "dns error is transient",
            inputErr:  &net.DNSError{Err: "server misbehaving", Name: "db.internal"},
            transient: true,
        },
        {
            name:      "net timeout error is transient",
            inputErr:  &stubTimeoutError{message: "operation stalled"},
            transient: true,
        },
        {
            name:      "net error with only the deprecated temporary flag is not transient",
            inputErr:  &stubTemporaryError{message: "resource momentarily busy"},
            transient: false,
        },
        {
            name:      "connection abort marker is transient",
            inputErr:  errors.New("write tcp 10.0.0.1:5432: software caused connection abort"),
            transient: true,
        },
        {
            name:      "windows connection abort marker is transient",
            inputErr:  errors.New("write tcp 10.0.0.1:5432: An established connection was aborted by the software in your host machine."),
            transient: true,
        },
        {
            name:      "connection refused marker is transient",
            inputErr:  errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"),
            transient: true,
        },
        {
            name:      "too many connections marker is transient",
            inputErr:  errors.New("FATAL: too many connections for role"),
            transient: true,
        },
        {
            name:      "broken pipe marker is transient",
            inputErr:  errors.New("write: broken pipe"),
            transient: true,
        },
        {
            name:      "syntax error is not transient",
            inputErr:  errors.New("ERROR: syntax error at or near \"select\" (SQLSTATE=42601)"),
            transient: false,
        },
        {
            name:      "authentication failure is not transient",
            inputErr:  errors.New("FATAL: password authentication failed for user"),
            transient: false,
        },
        {
            name:      "database starting up is transient",
            inputErr:  errors.New("FATAL: the database system is starting up (SQLSTATE=57P03)"),
            transient: true,
        },
        {
            name:      "database in recovery is transient",
            inputErr:  errors.New("FATAL: the database system is in recovery mode (SQLSTATE=57P03)"),
            transient: true,
        },
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            if testCase.transient != provider.isTransientError(testCase.inputErr) {
                t.Fatalf("expected isTransientError=%v for %v", testCase.transient, testCase.inputErr)
            }
        })
    }
}

func TestIsTransientErrorTraversesWrappedErrors(t *testing.T) {
    provider := newTestProvider()

    inputErr := &wrappedError{
        message: "database connection failed",
        cause: &wrappedError{
            message: "dial tcp: connect: network is unreachable",
            cause:   nil,
        },
    }

    if false == provider.isTransientError(inputErr) {
        t.Fatalf("expected the wrapped transient cause to be detected")
    }

    opaqueErr := &wrappedError{
        message: "database connection failed",
        cause: &wrappedError{
            message: "permission denied for schema",
            cause:   nil,
        },
    }

    if true == provider.isTransientError(opaqueErr) {
        t.Fatalf("expected the wrapped non-transient cause to stay non-transient")
    }
}

/* @info NaN fails every comparison, so a NaN multiplier would slip through a `1 > x` clamp, poison the float-space growth and convert to a negative duration — an immediate re-dial storm; the not-at-least-1 clamp resolves it to the default. */
func TestComputeBackoffDelayNaNMultiplierFallsBackToDefault(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(3, -time.Second, -time.Second, math.NaN()))

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a NaN multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the NaN fallback to keep the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}
