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
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
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
