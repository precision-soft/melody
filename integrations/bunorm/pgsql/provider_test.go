package pgsql

import (
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    "math"
    "net"
    "os"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/uptrace/bun/driver/pgdriver"
    "github.com/uptrace/bun/schema"
)

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
    parameters    map[string]string
    markedSecrets []string
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
    instance.markedSecrets = append(instance.markedSecrets, name)

    return true
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

var bunDiagnosticsPinRan bool

/* the routing is once per process, so this pin must own the first open of the test binary: it is declared before every other test of this file on purpose, the test files that sort before this one construct configurations without opening anything, and a repeated run in the same binary (-count above one) skips rather than reads a once another run consumed. The diagnostic is provoked through bun's public surface — a query carrying an argument with no placeholder — because what is pinned is that a retry-less open installs the journal as bun's destination, not that the adapter writes where it was pointed. */
func TestOpenContext_ARetrylessOpenRoutesBunDiagnosticsIntoTheJournal(t *testing.T) {
    if true == bunDiagnosticsPinRan {
        t.Skip("the process-wide routing once was consumed by an earlier run in this binary; the pin proves on the first run")
    }
    bunDiagnosticsPinRan = true

    logger := &capturingProviderLogger{}
    resolver := newStubResolverWithLogger(logger)

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(200*time.Millisecond, time.Second, time.Second))

    _, openErr := provider.OpenContext(context.Background(), resolver)
    if nil == openErr {
        t.Fatal("expected the open against an unreachable host to fail")
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    routed := false
    for _, record := range logger.entries {
        if "bun diagnostic" == record.message && strings.Contains(fmt.Sprintf("%v", record.context["line"]), "placeholders") {
            routed = true
        }
    }

    if false == routed {
        t.Fatal("the retry-less open did not route bun's diagnostics into the journal")
    }
}

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
    timeoutConfig := NewTimeoutConfig(time.Second, 0, 0)
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

func TestProviderOpenWithRetryAndNoLoggerServiceDoesNotPanic(t *testing.T) {
    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(100*time.Millisecond, 0, 0)).
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
        WithTimeoutConfig(NewTimeoutConfig(0, 0, 0))

    database, openErr := provider.Open(resolver)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout resolved to the default connect deadline against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}

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

func TestResolvedTimeoutConfig_NonPositiveConnectTimeoutFallsBackToTheDefault(t *testing.T) {
    defaultConfig := DefaultTimeoutConfig()

    if defaultConfig.ConnectTimeout != (&Provider{}).resolvedTimeoutConfig().ConnectTimeout {
        t.Fatalf("expected the default for a nil configuration")
    }

    if defaultConfig.ConnectTimeout != (&Provider{timeoutConfig: &TimeoutConfig{}}).resolvedTimeoutConfig().ConnectTimeout {
        t.Fatalf("expected the default for a zero-value configuration")
    }

    if defaultConfig.ConnectTimeout != (&Provider{timeoutConfig: NewTimeoutConfig(-1, 0, 0)}).resolvedTimeoutConfig().ConnectTimeout {
        t.Fatalf("expected the default for a negative connect timeout")
    }

    if 7*time.Second != (&Provider{timeoutConfig: NewTimeoutConfig(7*time.Second, 0, 0)}).resolvedTimeoutConfig().ConnectTimeout {
        t.Fatalf("expected the configured connect timeout to survive")
    }
}

func TestResolvedPoolConfig_NonPositiveFieldsFallBackToTheDefaults(t *testing.T) {
    defaultConfig := DefaultPoolConfig()

    fromZero := (&Provider{poolConfig: &PoolConfig{}}).resolvedPoolConfig()
    if defaultConfig.MaxOpenConnections != fromZero.MaxOpenConnections ||
        defaultConfig.MaxIdleConnections != fromZero.MaxIdleConnections ||
        defaultConfig.ConnectionMaxLifetime != fromZero.ConnectionMaxLifetime ||
        defaultConfig.ConnectionMaxIdleTime != fromZero.ConnectionMaxIdleTime {
        t.Fatalf("expected the defaults for a zero-value pool, got %+v", fromZero)
    }

    fromNegative := (&Provider{poolConfig: NewPoolConfig(-1, -1, -1, -1)}).resolvedPoolConfig()
    if defaultConfig.MaxOpenConnections != fromNegative.MaxOpenConnections ||
        defaultConfig.ConnectionMaxLifetime != fromNegative.ConnectionMaxLifetime {
        t.Fatalf("expected the defaults for a negative pool, got %+v", fromNegative)
    }

    configured := (&Provider{poolConfig: NewPoolConfig(3, 2, time.Minute, time.Second)}).resolvedPoolConfig()
    if 3 != configured.MaxOpenConnections || time.Minute != configured.ConnectionMaxLifetime {
        t.Fatalf("expected the configured pool to survive, got %+v", configured)
    }

    if defaultConfig.MaxOpenConnections != (&Provider{}).resolvedPoolConfig().MaxOpenConnections {
        t.Fatalf("expected the defaults for a nil pool configuration")
    }
}

func TestResolvedTimeoutConfigNormalizesNonPositiveReadAndWriteDeadlines(t *testing.T) {
    provider := &Provider{timeoutConfig: NewTimeoutConfig(time.Second, 0, -1)}

    resolved := provider.resolvedTimeoutConfig()

    if DefaultTimeoutConfig().ReadTimeout != resolved.ReadTimeout {
        t.Fatalf("expected the zero read deadline to take the default, got %v", resolved.ReadTimeout)
    }

    if DefaultTimeoutConfig().WriteTimeout != resolved.WriteTimeout {
        t.Fatalf("expected the negative write deadline to take the default, got %v", resolved.WriteTimeout)
    }
}

func TestResolvedTimeoutConfigKeepsTheLiftedDeadlinesForMigration(t *testing.T) {
    provider := &Provider{
        timeoutConfig:     migrationTimeoutConfig(NewTimeoutConfig(7*time.Second, 0, 0)),
        tunedForMigration: true,
    }

    resolved := provider.resolvedTimeoutConfig()

    if 0 != resolved.ReadTimeout {
        t.Fatalf("expected the migration read deadline to stay lifted, got %v", resolved.ReadTimeout)
    }

    if 0 != resolved.WriteTimeout {
        t.Fatalf("expected the migration write deadline to stay lifted, got %v", resolved.WriteTimeout)
    }

    if 7*time.Second != resolved.ConnectTimeout {
        t.Fatalf("expected the derived connect timeout to survive, got %v", resolved.ConnectTimeout)
    }
}

func TestMigrationProviderDerivesTheMigrationShape(t *testing.T) {
    base := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(9*time.Second, 0, 0)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0))

    derived := base.migrationProvider()

    if false == derived.tunedForMigration {
        t.Fatal("expected the derived provider to carry the migration mark")
    }

    if 2 != derived.poolConfig.MaxOpenConnections || 1 != derived.poolConfig.MaxIdleConnections {
        t.Fatalf("expected the sequential migration pool, got %+v", derived.poolConfig)
    }

    if 0 != derived.poolConfig.ConnectionMaxLifetime || 0 != derived.poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected no connection recycling mid-run, got %+v", derived.poolConfig)
    }

    if 0 != derived.timeoutConfig.ReadTimeout || 0 != derived.timeoutConfig.WriteTimeout {
        t.Fatalf("expected the lifted deadlines, got %+v", derived.timeoutConfig)
    }

    if 9*time.Second != derived.timeoutConfig.ConnectTimeout {
        t.Fatalf("expected the connect timeout to survive the derivation, got %v", derived.timeoutConfig.ConnectTimeout)
    }

    if false == derived.insecure {
        t.Fatal("expected the tls posture to survive the derivation")
    }

    if base.retryConfig != derived.retryConfig {
        t.Fatal("expected the retry policy to survive the derivation")
    }
}

func TestProviderOpenMarksThePasswordParameterSecret(t *testing.T) {
    resolver := newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0))

    database, openErr := provider.Open(resolver)
    if nil != database {
        _ = database.Close()
    }
    if nil == openErr {
        t.Fatal("expected the unreachable open to fail")
    }

    marked := false
    for _, name := range resolver.configuration.(*stubConfiguration).markedSecrets {
        if "database.password" == name {
            marked = true
        }
    }

    if false == marked {
        t.Fatalf("expected the provider to mark its password parameter secret, marked %v", resolver.configuration.(*stubConfiguration).markedSecrets)
    }
}

/* the migration open is the door the registry's bound context did not reach: OpenForMigration carried no context at all, so a db:migrate cancelled by a supervisor slept out the whole retry budget against a down database. The derived provider is the same one either way — only the context differs — so the sleep has to be cut here too. */
func TestProviderOpenForMigrationContextCancelsTheRetrySleep(t *testing.T) {
    resolver := newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.OpenForMigrationContext(ctx, resolver)
    elapsed := time.Since(start)

    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database from a cancelled migration open")
    }

    if nil == openErr {
        t.Fatal("expected the cancelled migration open to fail")
    }

    if false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the cancellation to be the cause, got %v", openErr)
    }

    if elapsed > time.Second {
        t.Fatalf("expected the cancellation to cut the retry sleep, took %v", elapsed)
    }

    /* the cause stays the cancellation, but the outage that was being retried arrives STRUCTURED beside it. Flattened into openErr.Error() it handed the operator a sentence and nothing to act on, while the retry warning one branch above lifted the same failure's context and cause chain — one record shape for the same failure, decided by whether the caller happened to cancel. */
    var melodyErr *exception.Error
    if false == errors.As(openErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the failed attempt, got %T", openErr)
    }

    errorContext := melodyErr.Context()

    if _, hasAttempt := errorContext["attempt"]; false == hasAttempt {
        t.Fatalf("expected the attempt in the record, got %v", errorContext)
    }

    if _, hasConnection := errorContext["connection"]; false == hasConnection {
        t.Fatalf("expected the failed attempt's own connection diagnostics beside the cancellation, got %v", errorContext)
    }
}

/* the context-less door stays what it was for every caller that holds no context: it is OpenForMigrationContext under a background one, which is why it can still be reached without changing a single call site */
func TestProviderOpenForMigrationRunsTheSameAttemptUnderABackgroundContext(t *testing.T) {
    resolver := newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0))

    database, openErr := provider.OpenForMigration(resolver)
    if nil != database {
        _ = database.Close()
    }

    if nil == openErr {
        t.Fatal("expected the unreachable migration open to fail")
    }

    if true == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected no cancellation from a background context, got %v", openErr)
    }
}

func TestProviderOpenContextCancelsTheRetrySleep(t *testing.T) {
    resolver := newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    provider := newTestProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.OpenContext(ctx, resolver)
    elapsed := time.Since(start)

    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database from a cancelled open")
    }

    if nil == openErr {
        t.Fatal("expected the cancelled open to fail")
    }

    if false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the cancellation to be the cause, got %v", openErr)
    }

    if elapsed > time.Second {
        t.Fatalf("expected the cancellation to cut the retry sleep, took %v", elapsed)
    }

    /* the cause stays the cancellation, but the outage that was being retried arrives STRUCTURED beside it. Flattened into openErr.Error() it handed the operator a sentence and nothing to act on, while the retry warning one branch above lifted the same failure's context and cause chain — one record shape for the same failure, decided by whether the caller happened to cancel. */
    var melodyErr *exception.Error
    if false == errors.As(openErr, &melodyErr) {
        t.Fatalf("expected a melody error carrying the failed attempt, got %T", openErr)
    }

    errorContext := melodyErr.Context()

    if _, hasAttempt := errorContext["attempt"]; false == hasAttempt {
        t.Fatalf("expected the attempt in the record, got %v", errorContext)
    }

    if _, hasConnection := errorContext["connection"]; false == hasConnection {
        t.Fatalf("expected the failed attempt's own connection diagnostics beside the cancellation, got %v", errorContext)
    }
}

func TestProviderDefaultReadDeadlineAllowsAnElevenSecondQuery(t *testing.T) {
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

    provider := newTestProvider(WithInsecure(true))

    database, openErr := provider.Open(resolver)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer database.Close()

    if _, queryErr := database.ExecContext(context.Background(), "SELECT pg_sleep(11)"); nil != queryErr {
        t.Fatalf("expected the default read deadline to allow a legitimately long query, got %v", queryErr)
    }
}

/* the pool half of the migration derivation survives the normalization the way the timeout half does: re-armed to the request defaults, the dedicated migration connections would be recycled mid-run — the cut OpenForMigration exists to prevent, by another name. */
func TestResolvedPoolConfigKeepsTheMigrationLifetimesLifted(t *testing.T) {
    provider := &Provider{
        poolConfig:        migrationPoolConfig(),
        tunedForMigration: true,
    }

    resolved := provider.resolvedPoolConfig()

    if 0 != resolved.ConnectionMaxLifetime {
        t.Fatalf("expected the migration connection lifetime to stay lifted, got %v", resolved.ConnectionMaxLifetime)
    }

    if 0 != resolved.ConnectionMaxIdleTime {
        t.Fatalf("expected the migration idle time to stay lifted, got %v", resolved.ConnectionMaxIdleTime)
    }

    if 2 != resolved.MaxOpenConnections || 1 != resolved.MaxIdleConnections {
        t.Fatalf("expected the migration pool sizing to survive, got %d/%d", resolved.MaxOpenConnections, resolved.MaxIdleConnections)
    }
}

/* the mysql mirror of the same rule, measured on the cancellation arriving MID-dial: the ping derives its budget from the caller's context, so a cancellation at two hundred milliseconds ends a ten-second dial right there — through a Background-derived ping it waited the whole connect budget out. The already-cancelled entry refusal is the other layer of the same rule; on this driver the derived ping shadows it for every at-entry input, which is why the in-flight cancellation is the input that proves the derivation. */
func TestOpenContext_ACancellationMidDialReachesTheAttemptInFlight(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(10*time.Second, 10*time.Second, 10*time.Second))

    resolver := newStubResolver("203.0.113.1", "5432", "melody", "melody", "melody")

    midFlightContext, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(200 * time.Millisecond)
        cancel()
    }()
    defer cancel()

    started := time.Now()
    _, openErr := provider.OpenContext(midFlightContext, resolver)
    elapsed := time.Since(started)

    if nil == openErr {
        t.Fatal("expected the cancelled open to fail")
    }

    if 2*time.Second < elapsed {
        t.Fatalf("expected the mid-dial cancellation to reach the attempt, waited %v", elapsed)
    }
}

func TestProvider_NamesThePasswordParameterAsItsOnlySecret(t *testing.T) {
    provider := NewProvider("database.host", "database.port", "database.name", "database.user", "database.password")

    names := provider.SecretParameterNames()
    if 1 != len(names) || "database.password" != names[0] {
        t.Fatalf("expected the password parameter as the only credential, got %v", names)
    }
}

type capturedProviderRecord struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

type capturingProviderLogger struct {
    entries []capturedProviderRecord
}

func (instance *capturingProviderLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.entries = append(instance.entries, capturedProviderRecord{level: level, message: message, context: context})
}

func (instance *capturingProviderLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingProviderLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingProviderLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingProviderLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingProviderLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

var _ loggingcontract.Logger = (*capturingProviderLogger)(nil)

type stubResolverWithLogger struct {
    *stubResolver
    logger loggingcontract.Logger
}

func (instance *stubResolverWithLogger) Get(serviceName string) (any, error) {
    if logging.ServiceLogger == serviceName {
        return instance.logger, nil
    }

    return instance.stubResolver.Get(serviceName)
}

func (instance *stubResolverWithLogger) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        panic(getErr)
    }

    return value
}

func (instance *stubResolverWithLogger) Has(serviceName string) bool {
    return logging.ServiceLogger == serviceName || instance.stubResolver.Has(serviceName)
}

func newStubResolverWithLogger(logger loggingcontract.Logger) *stubResolverWithLogger {
    return &stubResolverWithLogger{
        stubResolver: newStubResolver("203.0.113.1", "5432", "melody", "melody", "melody"),
        logger:       logger,
    }
}

/* the caller's own cancellation is a clean stop, not a database outage: the transient classifier carries no cancellation marker, so a shutdown that cancelled the open fell through to the terminal branch and paged the operator with "non-transient error" against a healthy database. */
func TestOpenWithRetry_ACancelledOpenIsAWarningRatherThanANonTransientOutage(t *testing.T) {
    logger := &capturingProviderLogger{}
    resolver := newStubResolverWithLogger(logger)

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(10*time.Second, 10*time.Second, 10*time.Second)).
        WithRetryConfig(DefaultRetryConfig())

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := provider.OpenContext(cancelledContext, resolver)
    if nil == openErr {
        t.Fatal("expected the cancelled open to fail")
    }

    if false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the cancellation to stay the cause, got %v", openErr)
    }

    if false == exception.IsAlreadyLogged(openErr) {
        t.Fatal("expected the recorded cancellation to carry the already-logged mark")
    }

    if 1 != len(logger.entries) {
        t.Fatalf("expected exactly one record for the cancelled open, got %d: %v", len(logger.entries), logger.entries)
    }

    record := logger.entries[0]

    if loggingcontract.LevelWarning != record.level {
        t.Fatalf("expected the cancellation at warning, got %v", record.level)
    }

    if "database open cancelled by the caller's context" != record.message {
        t.Fatalf("expected the cancellation named as itself, got %q", record.message)
    }
}

/* the cancellation that lands while an attempt waits out its backoff is the same clean stop, and it is recorded and marked here — an unmarked cancellation travelling up as a bare resolution failure is filed at error by whichever writer meets it. */
func TestOpenWithRetry_ACancellationDuringTheBackoffIsRecordedAndMarked(t *testing.T) {
    logger := &capturingProviderLogger{}
    resolver := &stubResolverWithLogger{
        stubResolver: newStubResolver("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        logger:       logger,
    }

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    _, openErr := provider.OpenContext(ctx, resolver)
    if nil == openErr {
        t.Fatal("expected the cancelled retry to fail")
    }

    if false == exception.IsAlreadyLogged(openErr) {
        t.Fatal("expected the recorded cancellation to carry the already-logged mark")
    }

    cancellationRecords := 0
    for _, entry := range logger.entries {
        if "database connection retry cancelled by the caller's context" == entry.message {
            cancellationRecords++

            if loggingcontract.LevelWarning != entry.level {
                t.Fatalf("expected the cancelled retry at warning, got %v", entry.level)
            }
        }
    }

    if 1 != cancellationRecords {
        t.Fatalf("expected exactly one record for the cancelled retry, got %d: %v", cancellationRecords, logger.entries)
    }
}

func TestOpenWithRetry_TheRetryWarningCarriesTheDiagnosticShapeTheTerminalRecordCarries(t *testing.T) {
    logger := &capturingProviderLogger{}

    resolver := &stubResolverWithLogger{
        stubResolver: newStubResolver("127.0.0.1", "1", "melody", "melody", "melody"),
        logger:       logger,
    }

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(time.Second, time.Second, time.Second)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 2*time.Millisecond, 2.0))

    _, openErr := provider.OpenContext(context.Background(), resolver)
    if nil == openErr {
        t.Fatal("expected the open against a refused port to fail")
    }

    retryRecordIndex := -1
    for index := range logger.entries {
        if "database connection failed and retrying" == logger.entries[index].message {
            retryRecordIndex = index

            break
        }
    }

    if -1 == retryRecordIndex {
        t.Fatalf("expected a retry warning, got %v", logger.entries)
    }

    retryContext := logger.entries[retryRecordIndex].context

    if _, exists := retryContext["connection"]; false == exists {
        t.Fatalf("expected the retry warning to name the connection the terminal record names, got %v", retryContext)
    }

    if _, exists := retryContext["timeoutConfig"]; false == exists {
        t.Fatalf("expected the retry warning to carry the deadlines that governed the attempt, got %v", retryContext)
    }

    if _, exists := retryContext["attempt"]; false == exists {
        t.Fatalf("expected the retry warning to keep its own attempt counter, got %v", retryContext)
    }

    if _, exists := retryContext["retryIn"]; false == exists {
        t.Fatalf("expected the retry warning to keep the delay it announces, got %v", retryContext)
    }
}

/* the transient markers are matched as WORDS, not as bare substrings. The short ones sit inside ordinary identifiers — "eof" inside a table named `geofences`, "timeout" inside a `session_timeout` column — and a permanent failure classified transient is retried for the whole budget before dying under "failed after max retry attempts" instead of "non-transient". */
func TestIsTransientError_AMarkerInsideAnIdentifierIsNotAMarker(t *testing.T) {
    provider := &Provider{}

    for _, permanentMessage := range []string{
        "Error 1146 (42S02): Table 'app.geofences' doesn't exist",
        "Error 1054 (42S22): Unknown column 'session_timeout' in 'field list'",
    } {
        if true == provider.isTransientError(errors.New(permanentMessage)) {
            t.Fatalf("a permanent failure must not be retried: %q", permanentMessage)
        }
    }
}

func TestIsTransientError_TheMarkersThemselvesStillMatch(t *testing.T) {
    provider := &Provider{}

    for _, transientMessage := range []string{
        "unexpected EOF",
        "read tcp 10.0.0.1:5432: i/o timeout",
        "dial tcp 10.0.0.1:5432: connect: connection refused",
        "invalid connection: bad connection",
        "commands out of sync; read timeout",
    } {
        if false == provider.isTransientError(errors.New(transientMessage)) {
            t.Fatalf("a transient failure must still be retried: %q", transientMessage)
        }
    }
}

/* the TLS posture is read where the DRIVER receives it, not only from the helper that computes it. The helper has its own test, but nothing observed that its answer reaches the connector, and the wiring is what decides whether a session is encrypted — a deleted assignment would have left every default connection in plaintext with the helper's test still green. The post-build hook is handed the very configuration the connector is built from, so it is the seam; it refuses afterwards, which stops the attempt before any dial. */
func openObservingTheTlsPosture(t *testing.T, providerOptions ...ProviderOption) *tls.Config {
    t.Helper()

    stopBeforeDial := errors.New("stop before the dial")

    var seenTlsConfig *tls.Config
    observingOptions := append(
        []ProviderOption{
            WithPostBuildHook(func(ctx context.Context, resolver containercontract.Resolver, connector *pgdriver.Connector) error {
                seenTlsConfig = connector.Config().TLSConfig

                return stopBeforeDial
            }),
        },
        providerOptions...,
    )

    provider := NewProvider("database.host", "database.port", "database.name", "database.user", "database.password", observingOptions...)

    _, openErr := provider.Open(newStubResolver("db.internal", "5432", "melody", "melody_user", "melody_password"))
    if false == errors.Is(openErr, stopBeforeDial) {
        t.Fatalf("expected the hook to stop the attempt before the dial, got %v", openErr)
    }

    return seenTlsConfig
}

func TestProviderOpen_TheDefaultPostureReachesTheDriverAsAVerifyingConfig(t *testing.T) {
    tlsConfig := openObservingTheTlsPosture(t)

    if nil == tlsConfig {
        t.Fatal("the default must reach the driver as a verifying config, not as plaintext")
    }

    if "db.internal" != tlsConfig.ServerName {
        t.Fatalf("expected the configured host as the name to verify against, got %q", tlsConfig.ServerName)
    }

    if tls.VersionTLS12 != tlsConfig.MinVersion {
        t.Fatalf("expected TLS 1.2 as the floor, got %d", tlsConfig.MinVersion)
    }

    if true == tlsConfig.InsecureSkipVerify {
        t.Fatal("the default must VERIFY the server certificate; an unverified session is the driver's own convenience spelling and is refused deliberately")
    }
}

func TestProviderOpen_TheInsecureOptOutReachesTheDriverAsPlaintext(t *testing.T) {
    if nil != openObservingTheTlsPosture(t, WithInsecure(true)) {
        t.Fatal("WithInsecure is the one plaintext path and must leave the driver without a TLS configuration")
    }
}

func TestProviderOpen_AnExplicitTlsConfigReachesTheDriverUntouched(t *testing.T) {
    pinnedTlsConfig := &tls.Config{ServerName: "pinned.example.com", MinVersion: tls.VersionTLS13}

    if pinnedTlsConfig != openObservingTheTlsPosture(t, WithTlsConfig(pinnedTlsConfig)) {
        t.Fatal("an explicit TLS configuration must reach the driver exactly as it was given")
    }
}

/* the diagnostic context of a failed connection carries the pool sizing and the deadlines that governed the attempt, the mysql sibling's shape, so the operator reading the record does not see only the address that refused. */
func TestToConnectionContextCarriesThePoolAndTimeoutConfiguration(t *testing.T) {
    provider := &Provider{}

    connectionContext := provider.toConnectionContext(
        NewConnectionConfig("host", "5432", "database", "user", "password"),
        DefaultPoolConfig(),
        DefaultTimeoutConfig(),
        "rewritten-host:5433",
    )

    for _, key := range []string{"connection", "poolConfig", "timeoutConfig", "dialedAddress"} {
        if _, exists := connectionContext[key]; false == exists {
            t.Fatalf("expected the connection context to carry %q, got %v", key, connectionContext)
        }
    }

    /* the address the dial reached is named apart from the configured one, because the post-build hook may have rewritten it after the connection config was built */
    if "rewritten-host:5433" != connectionContext["dialedAddress"] {
        t.Fatalf("expected the dialled endpoint, got %v", connectionContext["dialedAddress"])
    }
}


/*
TestComputeBackoffDelayFloorsASubMillisecondInitialDelay pins the floor. The
guards above refuse a non-positive delay, which left ONE NANOSECOND as the
smallest thing a configuration could ask for — and the wait it produces is
shorter than the dial it separates, so what the operator gets is the re-dial
storm those guards exist to prevent, arriving through the door they left open.

The growth is asserted from the floor as well as the floor itself: a fix that
clamped the ANSWER instead of the starting point would return the floor at every
attempt and stop backing off at all.
*/
func TestComputeBackoffDelayFloorsASubMillisecondInitialDelay(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(10, time.Nanosecond, 5*time.Second, 2.0))

    if time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected a one-nanosecond initial delay to be floored to 1ms, got %s", provider.computeBackoffDelay(1))
    }

    if 2*time.Millisecond != provider.computeBackoffDelay(2) {
        t.Fatalf("expected the backoff to keep growing from the floor, got %s", provider.computeBackoffDelay(2))
    }
}

/* the floor covers the CEILING too: a sub-millisecond max delay would otherwise cap a perfectly sane initial delay straight back under the floor, which is the same storm reached from the other field. */
func TestComputeBackoffDelayFloorsASubMillisecondCeiling(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(10, time.Second, time.Nanosecond, 2.0))

    if time.Millisecond != provider.computeBackoffDelay(1) {
        t.Fatalf("expected a one-nanosecond max delay to be floored to 1ms, got %s", provider.computeBackoffDelay(1))
    }
}

/*
TestComputeBackoffDelayAnswersAConstantMultiplierInBoundedTime is the guard on
the cost, and it is written as a DEADLINE because that is the only way the cost
is observable. A multiplier of exactly 1 is a valid constant backoff, and it is
the one value a growth walked attempt by attempt never leaves early: the delay
does not move, so the walk runs once per attempt already made and a run costs its
own square. At the largest attempt the counter can reach that walk is billions of
float multiplications; the closed form is a single one.

The window is MEASURED, not guessed: the walk it must not fit inside costs 1.02s
on the development container at the largest attempt, so 250ms separates the two
by four times in the failing direction while leaving the closed form — one
math.Pow — a quarter of a second of scheduling slack it can never need. A window
picked by eye at two seconds would have let the walk finish comfortably inside
it, which is a probe that certifies nothing.

The value is asserted beside the deadline so the probe cannot pass by answering
quickly and wrongly.
*/
func TestComputeBackoffDelayAnswersAConstantMultiplierInBoundedTime(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(0, 10*time.Millisecond, 5*time.Second, 1.0))

    answered := make(chan time.Duration, 1)
    go func() {
        answered <- provider.computeBackoffDelay(4000000000)
    }()

    select {
    case delay := <-answered:
        if 10*time.Millisecond != delay {
            t.Fatalf("expected a constant backoff to stay at the initial delay, got %s", delay)
        }
    case <-time.After(250 * time.Millisecond):
        t.Fatal("the constant backoff did not answer in bounded time: the growth is being walked one attempt at a time")
    }
}

/* an attempt of zero is not an attempt already made: it reads as the first one, which is the answer the growth this replaced gave it by never running. Without the reading the unsigned subtraction wraps to four billion steps of growth and the delay leaves for the ceiling. */
func TestComputeBackoffDelayReadsAZeroAttemptAsTheFirst(t *testing.T) {
    provider := newTestProvider().
        WithRetryConfig(NewRetryConfig(3, 100*time.Millisecond, 250*time.Millisecond, 2.0))

    if 100*time.Millisecond != provider.computeBackoffDelay(0) {
        t.Fatalf("expected a zero attempt to read as the first, got %s", provider.computeBackoffDelay(0))
    }
}
