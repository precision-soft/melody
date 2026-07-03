package mysql

import (
    "context"
    "errors"
    "net"
    "os"
    "reflect"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
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

func TestNewProviderStoresParameterNamesAndAppliesOptions(t *testing.T) {
    hook := func(ctx context.Context, resolver containercontract.Resolver, driverConfig *driver.Config) error {
        return nil
    }

    provider := newTestProvider(WithPostBuildHook(hook))

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

    if nil == provider.postBuildHook {
        t.Fatalf("expected WithPostBuildHook to set the post-build hook")
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
    timeoutConfig := NewTimeoutConfig(time.Second, 2*time.Second, 3*time.Second)
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

    var seenConfig *driver.Config
    provider := newTestProvider(
        WithPostBuildHook(func(ctx context.Context, resolver containercontract.Resolver, driverConfig *driver.Config) error {
            seenConfig = driverConfig

            return hookErr
        }),
    )

    resolver := newStubResolver("db.internal", "3306", "melody", "melody_user", "melody_password")

    database, openErr := provider.Open(resolver)
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle when the post-build hook fails")
    }

    if false == errors.Is(openErr, hookErr) {
        t.Fatalf("expected the hook error to be wrapped, got: %v", openErr)
    }

    if nil == seenConfig {
        t.Fatalf("expected the post-build hook to receive the driver config")
    }

    if "db.internal:3306" != seenConfig.Addr {
        t.Fatalf("expected the resolved address db.internal:3306, got %q", seenConfig.Addr)
    }
    if "melody" != seenConfig.DBName {
        t.Fatalf("expected the resolved database melody, got %q", seenConfig.DBName)
    }
    if "melody_user" != seenConfig.User {
        t.Fatalf("expected the resolved user melody_user, got %q", seenConfig.User)
    }
    if "melody_password" != seenConfig.Passwd {
        t.Fatalf("expected the resolved password to be passed to the driver, got %q", seenConfig.Passwd)
    }
}

/* @info openWithRetry must fall back to the emergency logger when the resolver has no logger service instead of panicking on the warning path */

func TestProviderOpenWithRetryAndNoLoggerServiceDoesNotPanic(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond)).
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
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql provider integration test")
    }

    dsnConfig, parseErr := driver.ParseDSN(dsn)
    if nil != parseErr {
        t.Fatalf("parse dsn: %v", parseErr)
    }

    host, port, splitErr := net.SplitHostPort(dsnConfig.Addr)
    if nil != splitErr {
        t.Skipf("MYSQL_DSN address %q is not host:port; skipping", dsnConfig.Addr)
    }

    resolver := newStubResolver(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd)

    provider := newTestProvider().
        WithTimeoutConfig(
            NewTimeoutConfig(0, 30*time.Second, 30*time.Second),
        )

    database, openErr := provider.Open(resolver)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}
