package mysql

import (
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    "io"
    "math"
    "net"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v4"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
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

func newTestProvider(config ProviderConfig) *Provider {
    /* the development mysql these behavioural tests dial speaks plain TCP, so the helper arms the insecure opt-out the way the example's wiring does; the verifying default is proven on its own in TestConnectionTlsConfig, which builds its providers directly */
    config.Insecure = true

    return NewProvider(config)
}

var bunDiagnosticsPinRan bool

/* the routing is once per process, so this pin must own the first open of the test binary: it is declared before every other test of this file on purpose, the test files that sort before this one construct configurations without opening anything, and a repeated run in the same binary (-count above one) skips rather than reads a once another run consumed. The diagnostic is provoked through bun's public surface — a query carrying an argument with no placeholder — because what is pinned is that a single-attempt open installs the journal as bun's destination, not that the adapter writes where it was pointed. */
func TestOpenContext_ASingleAttemptOpenRoutesBunDiagnosticsIntoTheJournal(t *testing.T) {
    if true == bunDiagnosticsPinRan {
        t.Skip("the process-wide routing once was consumed by an earlier run in this binary; the pin proves on the first run")
    }
    bunDiagnosticsPinRan = true

    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   singleAttemptRetry,
    })

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
        t.Fatal("the single-attempt open did not route bun's diagnostics into the journal")
    }
}

func TestOpenContext_TheDriverReceivesEveryDeadlineAndTheDefaultPort(t *testing.T) {
    var seen *driver.Config

    provider := NewProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(7 * time.Second), ReadTimeout: bound.UnlimitedDuration(), WriteTimeout: bound.Of(13 * time.Second)},
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            seen = driverConfig

            return errors.New("stop before the connection")
        },
        Retry: singleAttemptRetry,
    })

    _, openErr := provider.OpenContext(context.Background(), newTestParams("203.0.113.1", "", "melody", "melody", "melody"), &capturingProviderLogger{})
    if nil == openErr || nil == seen {
        t.Fatalf("expected the hook to see the driver configuration and end the open, got %v", openErr)
    }

    if "203.0.113.1:3306" != seen.Addr {
        t.Fatalf("expected the default port, got %q", seen.Addr)
    }

    if 7*time.Second != seen.Timeout || 0 != seen.ReadTimeout || 13*time.Second != seen.WriteTimeout {
        t.Fatalf("expected the dial, read and write deadlines 7s/none/13s on the driver, got %v/%v/%v", seen.Timeout, seen.ReadTimeout, seen.WriteTimeout)
    }
}

func TestNewProvider_TheZeroConfigurationResolvesToTheDefaults(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    if 25 != provider.pool.openConnectionCount() || 10*time.Second != provider.timeout.connectTimeout || 3 != provider.retry.maxAttempts {
        t.Fatalf("expected the default pool, deadlines and retry, got %+v / %+v / %+v", provider.pool, provider.timeout, provider.retry)
    }

    if nil != provider.postBuildHook || false != provider.insecure || nil != provider.tlsConfig {
        t.Fatalf("expected no hook and the verifying default")
    }
}

func TestNewProvider_KeepsTheConfiguredValues(t *testing.T) {
    hook := func(ctx context.Context, driverConfig *driver.Config) error {
        return nil
    }

    provider := newTestProvider(ProviderConfig{
        Pool:          PoolConfig{MaxOpenConnections: bound.Of(10), MaxIdleConnections: bound.Of(2)},
        Timeout:       TimeoutConfig{ConnectTimeout: bound.Of(time.Second), ReadTimeout: bound.Of(2 * time.Second)},
        Retry:         RetryConfig{MaxAttempts: bound.Of(4), BackoffMultiplier: 3.0},
        PostBuildHook: hook,
    })

    if 10 != provider.pool.openConnectionCount() || 2 != provider.pool.idleConnectionCount() {
        t.Fatalf("expected the configured pool, got %+v", provider.pool)
    }

    if time.Second != provider.timeout.connectTimeout || 2*time.Second != driverDuration(provider.timeout.readTimeout) {
        t.Fatalf("expected the configured deadlines, got %+v", provider.timeout)
    }

    if 4 != provider.retry.maxAttempts || 3.0 != provider.retry.backoffMultiplier {
        t.Fatalf("expected the configured retry, got %+v", provider.retry)
    }

    if nil == provider.postBuildHook || false == provider.insecure {
        t.Fatalf("expected the hook and the insecure opt-out kept")
    }
}

func TestNewProvider_PanicsNamingAFieldOutsideItsRule(t *testing.T) {
    expectPanicNamingField(t, "TimeoutConfig.ConnectTimeout", func() {
        NewProvider(ProviderConfig{Timeout: TimeoutConfig{ConnectTimeout: bound.UnlimitedDuration()}})
    })

    expectPanicNamingField(t, "RetryConfig.BackoffMultiplier", func() {
        NewProvider(ProviderConfig{Retry: RetryConfig{BackoffMultiplier: math.NaN()}})
    })
}

/* this major hands the connection values to the open rather than to the provider, so what has to travel into the migration derivation is everything the provider itself still holds: the hook, the retry policy and the transport settings, over the migration pool and the lifted deadlines. */
func TestMigrationProvider_KeepsHookRetryAndTransportOverTheMigrationShape(t *testing.T) {
    hookCalled := false
    explicitTlsConfig := &tls.Config{ServerName: "pinned.example.com"}

    provider := NewProvider(ProviderConfig{
        TlsConfig: explicitTlsConfig,
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            hookCalled = true
            return nil
        },
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), InitialDelay: bound.Of(time.Second), MaxDelay: bound.Of(10 * time.Second)},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(7 * time.Second), ReadTimeout: bound.Of(30 * time.Second), WriteTimeout: bound.Of(30 * time.Second)},
    })

    derived := provider.migrationProvider()

    if explicitTlsConfig != derived.tlsConfig || provider.insecure != derived.insecure {
        t.Fatalf("expected the transport settings to travel whole")
    }

    if provider.retry != derived.retry {
        t.Fatalf("expected the retry policy to travel whole, got %+v", derived.retry)
    }

    if nil == derived.postBuildHook {
        t.Fatalf("expected the post build hook to travel whole")
    }
    _ = derived.postBuildHook(context.Background(), driver.NewConfig())
    if false == hookCalled {
        t.Fatalf("expected the derived provider to carry the same hook")
    }

    if 7*time.Second != derived.timeout.connectTimeout || 0 != driverDuration(derived.timeout.readTimeout) || 0 != driverDuration(derived.timeout.writeTimeout) {
        t.Fatalf("expected the connect timeout kept and the deadlines lifted, got %+v", derived.timeout)
    }

    if 2 != derived.pool.openConnectionCount() || 0 != driverDuration(derived.pool.connectionMaxLifetime) {
        t.Fatalf("expected the migration pool, got %+v", derived.pool)
    }

    if 30*time.Second != driverDuration(provider.timeout.readTimeout) || 2 == provider.pool.openConnectionCount() {
        t.Fatalf("expected the derivation to leave the provider it derives from untouched")
    }
}

func TestProviderOpenBuildsDriverConfigAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConfig *driver.Config
    provider := newTestProvider(ProviderConfig{
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            seenConfig = driverConfig

            return hookErr
        },
        Retry: singleAttemptRetry,
    })

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
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond), ReadTimeout: bound.Of(100 * time.Millisecond), WriteTimeout: bound.Of(100 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(5 * time.Millisecond)},
    })

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

func TestProviderOpenWithAnAbsentConnectTimeoutConnects(t *testing.T) {
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

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ReadTimeout: bound.Of(30 * time.Second), WriteTimeout: bound.Of(30 * time.Second)},
        Retry:   singleAttemptRetry,
    })

    database, openErr := provider.Open(
        newTestParams(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd),
        nil,
    )
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
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{MaxAttempts: bound.Of(3), InitialDelay: bound.Of(100 * time.Millisecond), MaxDelay: bound.Of(250 * time.Millisecond)},
    })

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

func TestComputeBackoffDelayAbsentValuesTakeTheDefaults(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{},
    })

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

func TestIsTransientErrorClassification(t *testing.T) {
    provider := newTestProvider(ProviderConfig{Retry: singleAttemptRetry})

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
    provider := newTestProvider(ProviderConfig{Retry: singleAttemptRetry})

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

/* the migration open is the door the registry's bound context did not reach: OpenForMigration carried no context at all, so a db:migrate cancelled by a supervisor slept out the whole retry budget against a down database. The derived provider is the same one either way — only the context differs — so the sleep has to be cut here too. */
func TestProviderOpenForMigrationContextCancelsTheRetrySleep(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), InitialDelay: bound.Of(2 * time.Second), MaxDelay: bound.Of(2 * time.Second), BackoffMultiplier: 1.0},
    })

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

    /* the cause stays the cancellation, and the outage being retried arrives STRUCTURED beside it — the attempt and the failed attempt's connection diagnostics in the context — so the operator gets something to act on rather than a flattened sentence, whether or not the caller cancels. */
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
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   singleAttemptRetry,
    })

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
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), InitialDelay: bound.Of(2 * time.Second), MaxDelay: bound.Of(2 * time.Second), BackoffMultiplier: 1.0},
    })

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

    /* the cause stays the cancellation, and the outage being retried arrives STRUCTURED beside it — the attempt and the failed attempt's connection diagnostics in the context — so the operator gets something to act on rather than a flattened sentence, whether or not the caller cancels. */
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

func TestToConnectionContextCarriesThePoolAndTimeoutConfiguration(t *testing.T) {
    provider := NewProvider(ProviderConfig{Pool: PoolConfig{MaxOpenConnections: bound.UnlimitedCount()}})

    connectionContext := provider.toConnectionContext(
        newTestParams("host", "3306", "database", "user", "password"),
        "rewritten-host:5433",
    )

    for _, key := range []string{"connection", "poolConfig", "timeoutConfig", "dialedAddress"} {
        if _, exists := connectionContext[key]; false == exists {
            t.Fatalf("expected the connection context to carry %q, got %v", key, connectionContext)
        }
    }

    if "unlimited" != connectionContext["poolConfig"].(map[string]any)["maxOpenConnections"] || "10s" != connectionContext["timeoutConfig"].(map[string]any)["connectTimeout"] {
        t.Fatalf("expected the resolved limits named as the record reads them, got %v", connectionContext)
    }

    if _, carriesPassword := connectionContext["connection"].(map[string]any)["password"]; true == carriesPassword {
        t.Fatalf("expected the connection context without the password, got %v", connectionContext["connection"])
    }

    /* the address the dial reached is named apart from the configured one, because the post-build hook may have rewritten it after the parameters were read */
    if "rewritten-host:5433" != connectionContext["dialedAddress"] {
        t.Fatalf("expected the dialled endpoint, got %v", connectionContext["dialedAddress"])
    }
}

func TestPortOrDefault_AnEmptyPortDialsTheDriversDefault(t *testing.T) {
    if "3306" != portOrDefault("") || "6000" != portOrDefault("6000") {
        t.Fatalf("expected the empty port read as 3306 and a set one kept, got %q / %q", portOrDefault(""), portOrDefault("6000"))
    }
}

/* the hook and the ping derive their budgets from the caller's context: an already-cancelled OpenContext must fail at the attempt in flight instead of waiting a full connect budget out against an unreachable host. */
func TestOpenContext_ACancelledContextReachesTheAttemptInFlight(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(10 * time.Second), ReadTimeout: bound.Of(10 * time.Second), WriteTimeout: bound.Of(10 * time.Second)},
        Retry:   singleAttemptRetry,
    })

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

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(10 * time.Second), ReadTimeout: bound.Of(10 * time.Second), WriteTimeout: bound.Of(10 * time.Second)},
        Retry:   RetryConfig{},
    })

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

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), InitialDelay: bound.Of(2 * time.Second), MaxDelay: bound.Of(2 * time.Second), BackoffMultiplier: 1.0},
    })

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

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(time.Second), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(2 * time.Millisecond)},
    })

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
    defaultProvider := NewProvider(ProviderConfig{Retry: singleAttemptRetry})
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

    insecureProvider := NewProvider(ProviderConfig{Insecure: true, Retry: singleAttemptRetry})
    if nil != insecureProvider.connectionTlsConfig("db.example.com") {
        t.Fatalf("expected WithInsecure to disable TLS entirely (nil), got a config")
    }

    explicitConfig := &tls.Config{ServerName: "pinned.example.com"}
    explicitProvider := NewProvider(ProviderConfig{TlsConfig: explicitConfig, Retry: singleAttemptRetry})
    if explicitConfig != explicitProvider.connectionTlsConfig("db.example.com") {
        t.Fatalf("expected WithTlsConfig to win outright")
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
        "read tcp 10.0.0.1:3306: i/o timeout",
        "dial tcp 10.0.0.1:3306: connect: connection refused",
        "invalid connection: bad connection",
        "commands out of sync; read timeout",
    } {
        if false == provider.isTransientError(errors.New(transientMessage)) {
            t.Fatalf("a transient failure must still be retried: %q", transientMessage)
        }
    }
}

/* the TLS posture is read where the DRIVER receives it, not only from the helper that computes it: the wiring decides whether a session is encrypted, and a deleted assignment leaves every default connection in plaintext with the helper's own test still green. The post-build hook is handed the very configuration the connector is built from, so it is the seam; it refuses afterwards, which stops the attempt before any dial. */
func openObservingTheTlsPosture(t *testing.T, config ProviderConfig) *tls.Config {
    t.Helper()

    stopBeforeDial := errors.New("stop before the dial")

    var seenTlsConfig *tls.Config
    config.PostBuildHook = func(ctx context.Context, driverConfig *driver.Config) error {
        seenTlsConfig = driverConfig.TLS

        return stopBeforeDial
    }
    config.Retry = singleAttemptRetry

    provider := NewProvider(config)

    _, openErr := provider.Open(newTestParams("db.internal", "3306", "melody", "melody_user", "melody_password"), nil)
    if false == errors.Is(openErr, stopBeforeDial) {
        t.Fatalf("expected the hook to stop the attempt before the dial, got %v", openErr)
    }

    return seenTlsConfig
}

func TestProviderOpen_TheDefaultPostureReachesTheDriverAsAVerifyingConfig(t *testing.T) {
    tlsConfig := openObservingTheTlsPosture(t, ProviderConfig{})

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
    if nil != openObservingTheTlsPosture(t, ProviderConfig{Insecure: true}) {
        t.Fatal("WithInsecure is the one plaintext path and must leave the driver without a TLS configuration")
    }
}

func TestProviderOpen_AnExplicitTlsConfigReachesTheDriverUntouched(t *testing.T) {
    pinnedTlsConfig := &tls.Config{ServerName: "pinned.example.com", MinVersion: tls.VersionTLS13}

    if pinnedTlsConfig != openObservingTheTlsPosture(t, ProviderConfig{TlsConfig: pinnedTlsConfig}) {
        t.Fatal("an explicit TLS configuration must reach the driver exactly as it was given")
    }
}

func TestProviderOpenWithNilLoggerWritesTheTerminalRecordToStandardError(t *testing.T) {
    logging.CloseEmergencyLogger()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("stderr pipe: %v", pipeErr)
    }

    originalStderr := os.Stderr
    os.Stderr = writeEnd
    defer func() {
        os.Stderr = originalStderr
        logging.CloseEmergencyLogger()
    }()

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond), ReadTimeout: bound.Of(100 * time.Millisecond), WriteTimeout: bound.Of(100 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1), InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(5 * time.Millisecond)},
    })

    database, openErr := provider.Open(
        newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
        nil,
    )

    os.Stderr = originalStderr
    _ = writeEnd.Close()
    captured, readErr := io.ReadAll(readEnd)
    _ = readEnd.Close()
    if nil != readErr {
        t.Fatalf("read captured stderr: %v", readErr)
    }

    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle for an unreachable host")
    }

    if nil == openErr {
        t.Fatalf("expected a connection error for an unreachable host")
    }

    if false == exception.IsAlreadyLogged(openErr) {
        t.Fatalf("the terminal error must carry the logged mark")
    }

    if false == strings.Contains(string(captured), "database connection failed") {
        t.Fatalf("the terminal record must reach standard error when the logger is nil, got: %q", string(captured))
    }
}

/* TestComputeBackoffDelayAnswersAConstantMultiplierInBoundedTime guards the cost, written as a DEADLINE because that is the only way the cost is observable: a multiplier of exactly 1 is a valid constant backoff, and a growth walked attempt by attempt would cost its own square at the largest attempt, where the closed form is one math.Pow. The 250ms window is shorter than such a walk while leaving the closed form ample slack, and the value is asserted beside the deadline so the probe cannot pass by answering quickly and wrongly. */
func TestComputeBackoffDelayAnswersAConstantMultiplierInBoundedTime(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{InitialDelay: bound.Of(10 * time.Millisecond), MaxDelay: bound.Of(5 * time.Second), BackoffMultiplier: 1.0},
    })

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
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{MaxAttempts: bound.Of(3), InitialDelay: bound.Of(100 * time.Millisecond), MaxDelay: bound.Of(250 * time.Millisecond)},
    })

    if 100*time.Millisecond != provider.computeBackoffDelay(0) {
        t.Fatalf("expected a zero attempt to read as the first, got %s", provider.computeBackoffDelay(0))
    }
}

/* a bare IPv6 literal joined as host:port reads as an address with too many colons: pgdriver refuses it by name, go-sql-driver re-joins it into a bracketed host that does not exist and dials that through the whole retry budget. The literal is bracketed, one already bracketed is left alone, and a host name or an IPv4 literal is joined as it always was. */
func TestDialAddressOf_BracketsABareIpv6Literal(t *testing.T) {
    caseList := []struct {
        host     string
        expected string
    }{
        {"::1", "[::1]:3306"},
        {"2001:db8::5", "[2001:db8::5]:3306"},
        {"fe80::1%eth0", "[fe80::1%eth0]:3306"},
        {"[::1]", "[::1]:3306"},
        {"localhost", "localhost:3306"},
        {"10.0.0.1", "10.0.0.1:3306"},
        {"", ":3306"},
    }

    for _, testCase := range caseList {
        if actual := dialAddressOf(testCase.host, "3306"); testCase.expected != actual {
            t.Errorf("dialAddressOf(%q) = %q, wanted %q", testCase.host, actual, testCase.expected)
        }
    }
}

/* an unset host made the driver dial ":port", which is the local system: the open connected to whatever listened there with the configured credentials instead of failing. It is refused by name before the driver configuration is built. */
func TestProviderOpen_RefusesAnEmptyHostBeforeBuildingTheDriverConfig(t *testing.T) {
    provider := NewProvider(ProviderConfig{
        Insecure: true,
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            t.Fatal("the driver configuration was built for an empty host")

            return nil
        },
        Retry: singleAttemptRetry,
    })

    database, openErr := provider.Open(newTestParams("", "3306", "melody", "melody_user", "melody_password"), nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for an empty host")
    }

    if nil == openErr || false == strings.Contains(openErr.Error(), "the host is empty") {
        t.Fatalf("expected the refusal to name the host, got %v", openErr)
    }
}

/* an outage the provider could not get past — the single-attempt open of a closed port, and the retry budget spent on it — is filed under bunorm.ErrDatabaseUnreachable, the one class a read/write splitter absorbs by reading from the primary; a refusal given by name is not, so a misconfigured replica is refused instead of served from the primary in silence. The driver failure stays reachable under the class. */
func TestOpenContext_AnOutageIsFiledAsUnreachableAndARefusalIsNot(t *testing.T) {
    unreachable := newTestParams("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    singleAttempt := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond)}, Retry: singleAttemptRetry})
    database, openErr := singleAttempt.Open(unreachable, nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for a closed port")
    }

    if false == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected the single-attempt open of a closed port filed as unreachable, got %v", openErr)
    }

    var netErr net.Error
    if false == errors.As(openErr, &netErr) {
        t.Fatalf("expected the driver failure reachable under the class, got %v", openErr)
    }

    retrying := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond)}, Retry: RetryConfig{MaxAttempts: bound.Of(2), InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(time.Millisecond), BackoffMultiplier: 1}})
    database, openErr = retrying.Open(unreachable, nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for a closed port")
    }

    if false == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected the exhausted retry budget filed as unreachable, got %v", openErr)
    }

    if false == exception.IsAlreadyLogged(openErr) {
        t.Fatalf("expected the terminal record's mark kept on the unreachable failure, got %v", openErr)
    }

    database, openErr = singleAttempt.Open(newTestParams("", "3306", "melody", "melody", "melody"), nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for an empty host")
    }

    if nil == openErr || true == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected a refusal by name NOT filed as unreachable, got %v", openErr)
    }
}

/* serverRefusal builds the driver's own error type with the MESSAGE the server sends — one that quotes the operand, so a database named "timeout" or a user named "eof" made a permanent refusal an outage when the classifier read the message */
func serverRefusal(number uint16, sqlState string, message string) error {
    refusal := &driver.MySQLError{Number: number, Message: message}
    copy(refusal.SQLState[:], sqlState)

    return exception.NewError("database connection failed", nil, refusal)
}

func TestIsTransientError_AServerRefusalWithAnIdentityIsTerminalWhateverItsMessageSays(t *testing.T) {
    provider := newTestProvider(ProviderConfig{Retry: singleAttemptRetry})

    refusals := []error{
        serverRefusal(1049, "42000", "Unknown database 'timeout'"),
        serverRefusal(1045, "28000", "Access denied for user 'eof'@'172.18.0.5' (using password: YES)"),
        serverRefusal(1044, "42000", "Access denied for user 'melody'@'%' to database 'connection refused'"),
    }

    for _, refusal := range refusals {
        if true == provider.isTransientError(refusal) {
            t.Fatalf("expected the server refusal to be terminal whatever its message says, got transient for %v", refusal)
        }
    }
}

func TestIsTransientError_AServerOutageWithAnIdentityIsTransient(t *testing.T) {
    provider := newTestProvider(ProviderConfig{Retry: singleAttemptRetry})

    outages := []error{
        serverRefusal(1040, "08004", "Too many connections"),
        serverRefusal(1053, "08S01", "Server shutdown in progress"),
        serverRefusal(1159, "08S01", "Got timeout reading communication packets"),
        serverRefusal(9001, "HY000", "Max connect timeout reached while reaching hostgroup 0 after 10000ms"),
        serverRefusal(9002, "HY000", "The hostgroup 0 has no servers available"),
        serverRefusal(1203, "42000", "User melody already has more than 'max_user_connections' active connections"),
    }

    for _, outage := range outages {
        if false == provider.isTransientError(outage) {
            t.Fatalf("expected the server outage to be transient, got terminal for %v", outage)
        }
    }
}

/* a budget the caller had already spent refuses the open before the attempt with a deadline as its cause, which satisfies the timeout the classifier admits — so the caller's own expired budget was filed under the class the read/write splitter serves the primary for */
func TestOpenContext_ACallersExpiredDeadlineIsNotAnUnreachableDatabase(t *testing.T) {
    expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: singleAttemptRetry}).OpenContext(expired, newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"), nil)
    if nil == openErr {
        t.Fatal("expected the open under an expired deadline to be refused")
    }

    if true == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected the caller's expired budget NOT filed as an unreachable database, got %v", openErr)
    }

    if false == errors.Is(openErr, context.DeadlineExceeded) {
        t.Fatalf("expected the caller's deadline kept as the cause, got %v", openErr)
    }
}

/* with a retry policy the same refusal was classified transient by its timeout, retried once for nothing and reported as a retry the caller cancelled — two warnings for a caller whose context was done before the first attempt */
func TestOpenWithRetry_ACallersExpiredDeadlineIsACleanStopWithoutARetry(t *testing.T) {
    logger := &capturingProviderLogger{}

    expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: RetryConfig{}}).OpenContext(expired, newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"), logger)
    if nil == openErr || false == errors.Is(openErr, context.DeadlineExceeded) {
        t.Fatalf("expected the caller's deadline kept as the cause, got %v", openErr)
    }

    if 1 != len(logger.entries) || "database open cancelled by the caller's context" != logger.entries[0].message {
        t.Fatalf("expected one clean-stop warning and no retry, got %+v", logger.entries)
    }
}

/* against a live server, the refusal quotes the operand: a database named after a transient marker is still a refusal by name, handed back instead of filed as the outage a replica falls back to the primary for */
func TestOpenContext_ALiveServerRefusalSpelledAroundAMarkerIsNotFiledAsUnreachable(t *testing.T) {
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

    provider := newTestProvider(ProviderConfig{Timeout: TimeoutConfig{ConnectTimeout: bound.Of(5 * time.Second)}, Retry: singleAttemptRetry})

    for _, params := range []bunorm.ConnectionParameters{
        newTestParams(host, port, "timeout", dsnConfig.User, dsnConfig.Passwd),
        newTestParams(host, port, dsnConfig.DBName, "eof", "eof"),
    } {
        database, openErr := provider.Open(params, nil)
        if nil != database {
            _ = database.Close()
            t.Fatalf("expected the refusal for %s/%s", params.Database, params.User)
        }

        var serverErr *driver.MySQLError
        if false == errors.As(openErr, &serverErr) {
            t.Fatalf("expected the server's own refusal, got %v", openErr)
        }

        if true == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
            t.Fatalf("expected the refusal for %s/%s handed back by name, got it filed as unreachable: %v", params.Database, params.User, openErr)
        }
    }
}

/* the nil guards of this package read a value through isNilInterface alone: it answers true for the nil interface itself, so no `nil ==` needs to stand beside it, and true for a typed nil, which `nil ==` cannot see */
func TestIsNilInterface_AnswersTheNilInterfaceAndTheTypedNil(t *testing.T) {
    if false == isNilInterface(nil) {
        t.Fatalf("expected the nil interface to be read as nil")
    }

    var typedNil *testing.T
    if false == isNilInterface(typedNil) {
        t.Fatalf("expected a typed nil to be read as nil")
    }

    if true == isNilInterface(t) {
        t.Fatalf("expected a live pointer to be read as present")
    }
}

func TestOpenWithRetry_AnAbsentAttemptBudgetTakesTheDefaultBudget(t *testing.T) {
    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(time.Second), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   RetryConfig{InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(2 * time.Millisecond)},
    })

    if _, openErr := provider.OpenContext(context.Background(), newTestParams("127.0.0.1", "1", "melody", "melody", "melody"), logger); nil == openErr {
        t.Fatal("expected the open against a refused port to fail")
    }

    for _, entry := range logger.entries {
        if "database connection failed after max retry attempts" == entry.message {
            if 3 != entry.context["maxAttempts"] || 3 != entry.context["attempt"] {
                t.Fatalf("expected the default budget of %d attempts spent, got %v", 3, entry.context)
            }

            return
        }
    }

    t.Fatalf("expected the terminal record of a spent budget, got %v", logger.entries)
}

/* silentPeer listens on loopback, accepts every connection and never writes the server greeting, the peer a handshake read waits on for ever without a deadline */
func silentPeer(t *testing.T) (string, string) {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }

    var connectionMutex sync.Mutex
    var connectionList []net.Conn
    go func() {
        for {
            connection, acceptErr := listener.Accept()
            if nil != acceptErr {
                return
            }

            connectionMutex.Lock()
            connectionList = append(connectionList, connection)
            connectionMutex.Unlock()
        }
    }()

    t.Cleanup(func() {
        _ = listener.Close()

        connectionMutex.Lock()
        defer connectionMutex.Unlock()
        for _, connection := range connectionList {
            _ = connection.Close()
        }
    })

    host, port, splitErr := net.SplitHostPort(listener.Addr().String())
    if nil != splitErr {
        t.Fatalf("split the listener address: %v", splitErr)
    }

    return host, port
}

/* openAgainstASilentPeer runs the open and answers how long it took, failing the test when it is still blocked well past the bound it must hold */
func openAgainstASilentPeer(t *testing.T, open func() (*bun.DB, error)) (time.Duration, error) {
    t.Helper()

    type openOutcome struct {
        database *bun.DB
        err      error
    }

    outcome := make(chan openOutcome, 1)
    startedAt := time.Now()
    go func() {
        database, openErr := open()
        outcome <- openOutcome{database: database, err: openErr}
    }()

    select {
    case result := <-outcome:
        if nil != result.database {
            _ = result.database.Close()
            t.Fatal("expected no database handle against a peer that never greets")
        }

        return time.Since(startedAt), result.err
    case <-time.After(5 * time.Second):
        t.Fatal("expected the open to return inside the connect timeout, it is still blocked after 5s")
    }

    return 0, nil
}

/* the migration pool lifts the read deadline, so before the ping preceded bun's dialect query a peer that accepted the connection and never greeted held the open for ever; the dial and the handshake are now bounded by the connect timeout */
func TestProvider_OpenForMigrationReturnsInsideTheConnectTimeoutAgainstAPeerThatAcceptsAndStaysSilent(t *testing.T) {
    host, port := silentPeer(t)
    provider := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)}, Retry: singleAttemptRetry})
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    open := func() (*bun.DB, error) {
        return provider.OpenForMigrationContext(ctx, newTestParams(host, port, "melody", "melody", "melody"), nil)
    }

    elapsed, openErr := openAgainstASilentPeer(t, open)
    if nil == openErr {
        t.Fatal("expected the open against a silent peer to fail")
    }

    if 1500*time.Millisecond < elapsed {
        t.Fatalf("expected the open to return inside the 500ms connect timeout, it took %s", elapsed)
    }
}

/* the request pool's open is bounded by the connect timeout as well, not by its read deadline */
func TestProvider_OpenReturnsInsideTheConnectTimeoutAgainstASilentPeer(t *testing.T) {
    host, port := silentPeer(t)
    provider := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), ReadTimeout: bound.Of(30 * time.Second), WriteTimeout: bound.Of(30 * time.Second)}, Retry: singleAttemptRetry})
    open := func() (*bun.DB, error) {
        return provider.OpenContext(context.Background(), newTestParams(host, port, "melody", "melody", "melody"), nil)
    }

    elapsed, openErr := openAgainstASilentPeer(t, open)
    if nil == openErr {
        t.Fatal("expected the open against a silent peer to fail")
    }

    if 1500*time.Millisecond < elapsed {
        t.Fatalf("expected the open to return inside the 500ms connect timeout, not the 30s read deadline, it took %s", elapsed)
    }
}

/* the default verifying TLS config is checked against the host without the brackets of an IPv6 literal and without the zone of a scoped one, since no certificate carries either; a host name or a plain address is checked as configured */
func TestTlsServerNameOf_DropsTheBracketsAndTheZoneOfAnIpv6Literal(t *testing.T) {
    for host, expected := range map[string]string{
        "fe80::1%eth0":     "fe80::1",
        "[::1]":            "::1",
        "[fe80::1%25eth0]": "fe80::1",
        "db.example.com":   "db.example.com",
        "10.0.0.7":         "10.0.0.7",
        "2001:db8::7":      "2001:db8::7",
    } {
        if serverName := tlsServerNameOf(host); expected != serverName {
            t.Fatalf("host %q: expected the name %q verified, got %q", host, expected, serverName)
        }
    }
}

/* the verifying default is built with the name tlsServerNameOf answers, so a scoped literal the dialer reaches can also be verified */
func TestConnectionTlsConfig_TheDefaultVerifiesAScopedLiteralWithoutItsZone(t *testing.T) {
    provider := NewProvider(ProviderConfig{Retry: singleAttemptRetry})

    if tlsConfig := provider.connectionTlsConfig("fe80::1%eth0"); nil == tlsConfig || "fe80::1" != tlsConfig.ServerName {
        t.Fatalf("expected the default config to verify fe80::1, got %+v", tlsConfig)
    }
}

func TestOpenContext_AnOpenHandedNoLoggerLeavesBunDiagnosticsWhereTheLastRoutingPutThem(t *testing.T) {
    t.Cleanup(bunorm.ResetDiagnostics)

    journal := &capturingProviderLogger{}
    bunorm.RouteDiagnostics(journal)

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   singleAttemptRetry,
    })

    _, openErr := provider.OpenContext(
        context.Background(),
        newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"),
        nil,
    )
    if nil == openErr {
        t.Fatal("expected the open against an unreachable host to fail")
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    routed := false
    for _, record := range journal.entries {
        if "bun diagnostic" == record.message && strings.Contains(fmt.Sprintf("%v", record.context["line"]), "placeholders") {
            routed = true
        }
    }

    if false == routed {
        t.Fatal("an open handed no logger moved bun's diagnostics away from the journal the last routing put them in")
    }
}

func TestOpenContext_AnAlreadyCancelledOpenLeavesBunDiagnosticsWhereTheLastRoutingPutThem(t *testing.T) {
    t.Cleanup(bunorm.ResetDiagnostics)

    journal := &capturingProviderLogger{}
    bunorm.RouteDiagnostics(journal)

    handed := &capturingProviderLogger{}
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: singleAttemptRetry}).OpenContext(
        cancelledContext,
        newTestParams("203.0.113.1", "3306", "melody", "melody", "melody"),
        handed,
    )
    if nil == openErr {
        t.Fatal("expected the open under a cancelled context to be refused")
    }

    _ = schema.SafeQuery("SELECT 1", []any{42})

    for _, record := range handed.entries {
        if "bun diagnostic" == record.message {
            t.Fatalf("a refused open took bun's diagnostics onto the logger it was handed: %+v", record)
        }
    }

    routed := false
    for _, record := range journal.entries {
        if "bun diagnostic" == record.message && strings.Contains(fmt.Sprintf("%v", record.context["line"]), "placeholders") {
            routed = true
        }
    }

    if false == routed {
        t.Fatal("a refused open moved bun's diagnostics away from the journal the last routing put them in")
    }
}

var countingDialSequence atomic.Int64

/* registerCountingDial takes a network name of its own per call, since the driver's registry is process-wide */
func registerCountingDial(t *testing.T) (string, *atomic.Int64) {
    t.Helper()

    name := fmt.Sprintf("melody-counting-dial-%d", countingDialSequence.Add(1))
    dials := &atomic.Int64{}

    driver.RegisterDialContext(name, func(ctx context.Context, address string) (net.Conn, error) {
        dials.Add(1)

        return (&net.Dialer{}).DialContext(ctx, "tcp", address)
    })

    return name, dials
}

func TestOpenWithRetry_ASingleAttemptDialsOnceAndFilesTheOutageAsUnreachable(t *testing.T) {
    network, dials := registerCountingDial(t)
    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(time.Second)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            driverConfig.Net = network

            return nil
        },
    })

    _, openErr := provider.OpenContext(context.Background(), newTestParams("127.0.0.1", "1", "melody", "melody", "melody"), logger)
    if false == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected the refused port filed as unreachable, got %v", openErr)
    }

    if 1 != dials.Load() {
        t.Fatalf("expected a single attempt to dial once, got %d dials", dials.Load())
    }

    for _, entry := range logger.entries {
        if "database connection failed after max retry attempts" == entry.message {
            if 1 != entry.context["maxAttempts"] || 1 != entry.context["attempt"] {
                t.Fatalf("expected one attempt of a budget of one, got %v", entry.context)
            }

            return
        }
    }

    t.Fatalf("expected the terminal record of the single attempt, got %v", logger.entries)
}

func TestOpenContext_AZeroIdleCapBootsOnThePingConnectionAndKeepsNoIdleConnectionAfter(t *testing.T) {
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

    network, dials := registerCountingDial(t)

    provider := newTestProvider(ProviderConfig{
        Pool:  PoolConfig{MaxIdleConnections: bound.Of(0)},
        Retry: singleAttemptRetry,
        PostBuildHook: func(ctx context.Context, driverConfig *driver.Config) error {
            driverConfig.Net = network

            return nil
        },
    })

    database, openErr := provider.Open(newTestParams(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd), nil)
    if nil != openErr {
        t.Fatalf("expected the open to succeed, got %v", openErr)
    }
    defer database.Close()

    if 1 != dials.Load() {
        t.Fatalf("expected the dialect's version query to run over the ping's connection, got %d dials", dials.Load())
    }

    if idle := database.DB.Stats().Idle; 0 != idle {
        t.Fatalf("expected the configured zero idle cap after the boot, got %d idle connections", idle)
    }
}
