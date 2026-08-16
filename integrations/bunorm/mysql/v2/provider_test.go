package mysql

import (
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    "math"
    "net"
    "os"
    "strings"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v2"
    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/uptrace/bun/schema"
)

func newTestParams(host string, port string, database string, user string, password string) bunorm.ConnectionParameters {
    return bunorm.ConnectionParameters{
        Host:     host,
        Port:     port,
        Database: database,
        User:     user,
        Password: password,
    }
}

func newTestProvider(providerOptions ...ProviderOption) *Provider {
    /* the development mysql these behavioural tests dial speaks plain TCP, so the helper arms the insecure opt-out the way the example's wiring does; the verifying default is proven on its own in TestConnectionTlsConfig, which builds its providers directly */
    return NewProvider(
        append([]ProviderOption{WithInsecure(true)}, providerOptions...)...,
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

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(200*time.Millisecond, time.Second, time.Second))

    _, openErr := provider.OpenContext(
        context.Background(),
        newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"),
        logger,
    )
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

func TestNewProviderAppliesOptions(t *testing.T) {
    hook := func(ctx context.Context, driverConfig *driver.Config) error {
        return nil
    }

    provider := newTestProvider(WithPostBuildHook(hook))

    if nil == provider.postBuildHook {
        t.Fatalf("expected WithPostBuildHook to set the post-build hook")
    }

    if nil != provider.poolConfig || nil != provider.timeoutConfig || nil != provider.retryConfig {
        t.Fatalf("expected pool/timeout/retry configs to default to nil")
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

func TestProviderOpenBuildsDriverConfigAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConfig *driver.Config
    provider := newTestProvider(
        WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
            seenConfig = driverConfig

            return hookErr
        }),
    )

    database, openErr := provider.Open(
        newTestParams("db.internal", "3306", "melody", "melody_user", "melody_password"),
        nil,
    )
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

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0))

    database, openErr := provider.Open(
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        nil,
    )
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

    provider := newTestProvider().
        WithTimeoutConfig(
            NewTimeoutConfig(0, 30*time.Second, 30*time.Second),
        )

    database, openErr := provider.Open(
        newTestParams(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd),
        nil,
    )
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout resolved to the default connect deadline against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}

func TestMigrationTimeoutConfig_LiftsDeadlinesKeepsConnect(t *testing.T) {
    derived := migrationTimeoutConfig(&TimeoutConfig{
        ConnectTimeout: 7 * time.Second,
        ReadTimeout:    30 * time.Second,
        WriteTimeout:   30 * time.Second,
    })

    if 7*time.Second != derived.ConnectTimeout {
        t.Fatalf("expected the connect timeout kept, got %v", derived.ConnectTimeout)
    }
    if 0 != derived.ReadTimeout || 0 != derived.WriteTimeout {
        t.Fatalf("expected the read and write deadlines lifted, got %v/%v", derived.ReadTimeout, derived.WriteTimeout)
    }

    derivedFromNil := migrationTimeoutConfig(nil)
    if DefaultTimeoutConfig().ConnectTimeout != derivedFromNil.ConnectTimeout {
        t.Fatalf("expected the default connect timeout for a nil base, got %v", derivedFromNil.ConnectTimeout)
    }
    if 0 != derivedFromNil.ReadTimeout || 0 != derivedFromNil.WriteTimeout {
        t.Fatalf("expected the deadlines lifted for a nil base")
    }
}

func TestMigrationPoolConfig_NeverRecyclesMidRun(t *testing.T) {
    poolConfig := migrationPoolConfig()

    if 0 != poolConfig.ConnectionMaxLifetime || 0 != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected no connection recycling for migrations, got %v/%v", poolConfig.ConnectionMaxLifetime, poolConfig.ConnectionMaxIdleTime)
    }
    if 2 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected the two connections a sequential migration run needs, got %d", poolConfig.MaxOpenConnections)
    }
}

/* this major hands the connection values to the open rather than to the provider, so what has to travel into the migration derivation is everything the provider itself still holds: the hook, the retry policy and the transport settings. */
func TestMigrationProviderKeepsHookRetryAndTransport(t *testing.T) {
    hookCalled := false
    retryConfig := NewRetryConfig(5, time.Second, 10*time.Second, 2.0)
    explicitTlsConfig := &tls.Config{ServerName: "pinned.example.com"}

    provider := NewProvider(
        WithTlsConfig(explicitTlsConfig),
        WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
            hookCalled = true
            return nil
        }),
    ).WithRetryConfig(retryConfig).WithTimeoutConfig(NewTimeoutConfig(7*time.Second, 30*time.Second, 30*time.Second))

    derived := provider.migrationProvider()

    if explicitTlsConfig != derived.tlsConfig {
        t.Fatalf("expected the transport settings to travel whole")
    }

    if retryConfig != derived.retryConfig {
        t.Fatalf("expected the retry policy to travel whole")
    }

    if nil == derived.postBuildHook {
        t.Fatalf("expected the post build hook to travel whole")
    }
    _ = derived.postBuildHook(context.Background(), driver.NewConfig())
    if false == hookCalled {
        t.Fatalf("expected the derived provider to carry the same hook")
    }

    if 7*time.Second != derived.timeoutConfig.ConnectTimeout || 0 != derived.timeoutConfig.ReadTimeout {
        t.Fatalf("expected the migration timeout derivation, got %+v", derived.timeoutConfig)
    }

    if 2 != derived.poolConfig.MaxOpenConnections {
        t.Fatalf("expected the migration pool derivation, got %+v", derived.poolConfig)
    }
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
            name:      "connection abort marker is transient",
            inputErr:  errors.New("write tcp 10.0.0.1:3306: software caused connection abort"),
            transient: true,
        },
        {
            name:      "windows connection abort marker is transient",
            inputErr:  errors.New("write tcp 10.0.0.1:3306: An established connection was aborted by the software in your host machine."),
            transient: true,
        },
        {
            name:      "connection refused marker is transient",
            inputErr:  errors.New("dial tcp 127.0.0.1:3306: connect: connection refused"),
            transient: true,
        },
        {
            name:      "too many connections marker is transient",
            inputErr:  errors.New("Error 1040: Too many connections"),
            transient: true,
        },
        {
            name:      "broken pipe marker is transient",
            inputErr:  errors.New("write: broken pipe"),
            transient: true,
        },
        {
            name:      "syntax error is not transient",
            inputErr:  errors.New("Error 1064: You have an error in your SQL syntax"),
            transient: false,
        },
        {
            name:      "access denied is not transient",
            inputErr:  errors.New("Error 1045: Access denied for user"),
            transient: false,
        },
        {
            name:      "server shutdown in progress is transient",
            inputErr:  errors.New("Error 1053: Server shutdown in progress"),
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

func TestResolvedTimeoutConfig_NonPositiveFieldsFallBackToTheDefaults(t *testing.T) {
    defaultConfig := DefaultTimeoutConfig()

    fromNil := (&Provider{}).resolvedTimeoutConfig()
    if defaultConfig.ConnectTimeout != fromNil.ConnectTimeout ||
        defaultConfig.ReadTimeout != fromNil.ReadTimeout ||
        defaultConfig.WriteTimeout != fromNil.WriteTimeout {
        t.Fatalf("expected the defaults for a nil configuration, got %+v", fromNil)
    }

    fromZero := (&Provider{timeoutConfig: &TimeoutConfig{}}).resolvedTimeoutConfig()
    if defaultConfig.ConnectTimeout != fromZero.ConnectTimeout {
        t.Fatalf("expected the default connect timeout for a zero-value configuration, got %v", fromZero.ConnectTimeout)
    }
    if defaultConfig.ReadTimeout != fromZero.ReadTimeout || defaultConfig.WriteTimeout != fromZero.WriteTimeout {
        t.Fatalf("expected the default deadlines for a zero-value configuration, got %v/%v", fromZero.ReadTimeout, fromZero.WriteTimeout)
    }

    /* a negative deadline puts it in the past: every dial fails instantly with an i/o timeout no network event caused, and the retry loop burns its attempts against a healthy server */
    fromNegative := (&Provider{timeoutConfig: NewTimeoutConfig(-1, -1, -1)}).resolvedTimeoutConfig()
    if defaultConfig.ConnectTimeout != fromNegative.ConnectTimeout ||
        defaultConfig.ReadTimeout != fromNegative.ReadTimeout ||
        defaultConfig.WriteTimeout != fromNegative.WriteTimeout {
        t.Fatalf("expected the defaults for negative values, got %+v", fromNegative)
    }

    /* a configured positive value is never touched */
    configured := (&Provider{timeoutConfig: NewTimeoutConfig(7*time.Second, 11*time.Second, 13*time.Second)}).resolvedTimeoutConfig()
    if 7*time.Second != configured.ConnectTimeout || 11*time.Second != configured.ReadTimeout || 13*time.Second != configured.WriteTimeout {
        t.Fatalf("expected the configured values to survive, got %+v", configured)
    }
}

func TestResolvedPoolConfig_NonPositiveFieldsFallBackToTheDefaults(t *testing.T) {
    defaultConfig := DefaultPoolConfig()

    fromNil := (&Provider{}).resolvedPoolConfig()
    if defaultConfig.MaxOpenConnections != fromNil.MaxOpenConnections {
        t.Fatalf("expected the defaults for a nil configuration, got %+v", fromNil)
    }

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
    if 3 != configured.MaxOpenConnections || 2 != configured.MaxIdleConnections ||
        time.Minute != configured.ConnectionMaxLifetime || time.Second != configured.ConnectionMaxIdleTime {
        t.Fatalf("expected the configured pool to survive, got %+v", configured)
    }
}

func TestResolvedConfigs_DoNotUndoTheMigrationTuning(t *testing.T) {
    derived := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(7*time.Second, 30*time.Second, 30*time.Second)).
        migrationProvider()

    timeoutConfig := derived.resolvedTimeoutConfig()
    if 0 != timeoutConfig.ReadTimeout || 0 != timeoutConfig.WriteTimeout {
        t.Fatalf("expected the migration deadlines to stay lifted, got %v/%v", timeoutConfig.ReadTimeout, timeoutConfig.WriteTimeout)
    }
    if 7*time.Second != timeoutConfig.ConnectTimeout {
        t.Fatalf("expected the connect timeout to stay armed, got %v", timeoutConfig.ConnectTimeout)
    }

    poolConfig := derived.resolvedPoolConfig()
    if 0 != poolConfig.ConnectionMaxLifetime || 0 != poolConfig.ConnectionMaxIdleTime {
        t.Fatalf("expected the migration pool to keep its recycling disabled, got %v/%v", poolConfig.ConnectionMaxLifetime, poolConfig.ConnectionMaxIdleTime)
    }
    if 2 != poolConfig.MaxOpenConnections {
        t.Fatalf("expected the migration pool size, got %d", poolConfig.MaxOpenConnections)
    }
}

/* the migration open is the door the registry's bound context did not reach: OpenForMigration carried no context at all, so a db:migrate cancelled by a supervisor slept out the whole retry budget against a down database. The derived provider is the same one either way — only the context differs — so the sleep has to be cut here too. */
func TestProviderOpenForMigrationContextCancelsTheRetrySleep(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.OpenForMigrationContext(
        ctx,
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        nil,
    )
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
}

/* the context-less door stays what it was for every caller that holds no context: it is OpenForMigrationContext under a background one, which is why it can still be reached without changing a single call site */
func TestProviderOpenForMigrationRunsTheSameAttemptUnderABackgroundContext(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0))

    database, openErr := provider.OpenForMigration(
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        nil,
    )
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
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.OpenContext(
        ctx,
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        nil,
    )
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

/* the diagnostic context of a failed connection carries the pool sizing and the deadlines that governed the attempt, the pgsql sibling's shape, so the operator reading the record does not see only the address that refused. */
func TestToConnectionContextCarriesThePoolAndTimeoutConfiguration(t *testing.T) {
    provider := &Provider{}

    connectionContext := provider.toConnectionContext(
        NewConnectionConfig("host", "3306", "database", "user", "password"),
        DefaultPoolConfig(),
        DefaultTimeoutConfig(),
    )

    for _, key := range []string{"connection", "poolConfig", "timeoutConfig"} {
        if _, exists := connectionContext[key]; false == exists {
            t.Fatalf("expected the connection context to carry %q, got %v", key, connectionContext)
        }
    }
}

/* the hook and the ping derive their budgets from the caller's context: an already-cancelled OpenContext must fail at the attempt in flight instead of waiting a full connect budget out against an unreachable host. */
func TestOpenContext_ACancelledContextReachesTheAttemptInFlight(t *testing.T) {
    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(10*time.Second, 10*time.Second, 10*time.Second))

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    started := time.Now()
    _, openErr := provider.OpenContext(
        cancelledContext,
        newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"),
        nil,
    )
    elapsed := time.Since(started)

    if nil == openErr {
        t.Fatal("expected the cancelled open to fail")
    }

    if 2*time.Second < elapsed {
        t.Fatalf("expected the cancellation to reach the attempt in flight, waited %v", elapsed)
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

/* the caller's own cancellation is a clean stop, not a database outage: the transient classifier carries no cancellation marker, so a shutdown that cancelled the open fell through to the terminal branch and paged the operator with "non-transient error" against a healthy database. */
func TestOpenWithRetry_ACancelledOpenIsAWarningRatherThanANonTransientOutage(t *testing.T) {
    logger := &capturingProviderLogger{}

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(10*time.Second, 10*time.Second, 10*time.Second)).
        WithRetryConfig(DefaultRetryConfig())

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := provider.OpenContext(
        cancelledContext,
        newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"),
        logger,
    )
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

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(50*time.Millisecond, 0, 0)).
        WithRetryConfig(NewRetryConfig(5, 2*time.Second, 2*time.Second, 1.0))

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    _, openErr := provider.OpenContext(
        ctx,
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        logger,
    )
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

    provider := newTestProvider().
        WithTimeoutConfig(NewTimeoutConfig(time.Second, time.Second, time.Second)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 2*time.Millisecond, 2.0))

    _, openErr := provider.OpenContext(
        context.Background(),
        newTestParams("127.0.0.1", "1", "melody", "melody", "melody"),
        logger,
    )
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

/* the provider negotiates a verifying TLS session by default, disables TLS only on WithInsecure, and hands a caller's explicit config through WithTlsConfig — the connection is never plaintext by omission. */
func TestConnectionTlsConfig(t *testing.T) {
    defaultProvider := NewProvider()
    tlsConfig := defaultProvider.connectionTlsConfig("db.example.com")
    if nil == tlsConfig {
        t.Fatalf("expected a verifying TLS config by default, got nil (plaintext)")
    }
    if "db.example.com" != tlsConfig.ServerName {
        t.Fatalf("expected the host as the verified server name, got %q", tlsConfig.ServerName)
    }
    if tls.VersionTLS12 != tlsConfig.MinVersion {
        t.Fatalf("expected a TLS 1.2 floor, got %d", tlsConfig.MinVersion)
    }
    if true == tlsConfig.InsecureSkipVerify {
        t.Fatalf("the default config must verify the server certificate")
    }

    insecureProvider := NewProvider(WithInsecure(true))
    if nil != insecureProvider.connectionTlsConfig("db.example.com") {
        t.Fatalf("expected WithInsecure to disable TLS entirely (nil), got a config")
    }

    explicitConfig := &tls.Config{ServerName: "pinned.example.com"}
    explicitProvider := NewProvider(WithTlsConfig(explicitConfig))
    if explicitConfig != explicitProvider.connectionTlsConfig("db.example.com") {
        t.Fatalf("expected WithTlsConfig to win outright")
    }
}
