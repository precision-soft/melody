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
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/schema"
)

func newTestParameters(host string, port string, database string, user string, password string) bunorm.ConnectionParameters {
    return bunorm.ConnectionParameters{
        Host:     host,
        Port:     port,
        Database: database,
        User:     user,
        Password: password,
    }
}

func newTestProvider(config ProviderConfig) *Provider {
    /* the development mysql speaks plain TCP, so the insecure opt-out is armed as the example's wiring does */
    config.Insecure = true

    return NewProvider(config)
}

var bunDiagnosticsPinRan bool

/* the routing is once per process, so this pin must own the first open of the test binary and is declared first */
func TestOpen_ASingleAttemptOpenRoutesBunDiagnosticsIntoTheJournal(t *testing.T) {
    if true == bunDiagnosticsPinRan {
        t.Skip("the process-wide routing once was consumed by an earlier run in this binary; the pin proves on the first run")
    }
    bunDiagnosticsPinRan = true

    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   singleAttemptRetry,
    })

    _, openErr := provider.Open(
        testRuntimeUnder(context.Background()),
        newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"),
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

func TestOpen_TheDriverReceivesEveryDeadlineAndTheDefaultPort(t *testing.T) {
    var seen *driver.Config

    provider := NewProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(7 * time.Second), ReadTimeout: bound.UnlimitedDuration(), WriteTimeout: bound.Of(13 * time.Second)},
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            seen = driverConfig

            return errors.New("stop before the connection")
        },
        Retry: singleAttemptRetry,
    })

    _, openErr := provider.Open(testRuntimeUnder(context.Background()), newTestParameters("203.0.113.1", "", "melody", "melody", "melody"), &capturingProviderLogger{})
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
    hook := func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
        return nil
    }

    provider := newTestProvider(ProviderConfig{
        Pool:          PoolConfig{MaxOpenConnections: bound.Of(10), MaxIdleConnections: bound.Of(2)},
        Timeout:       TimeoutConfig{ConnectTimeout: bound.Of(time.Second), ReadTimeout: bound.Of(2 * time.Second)},
        Retry:         RetryConfig{MaxAttempts: bound.Of(4), Backoff: bound.Backoff{Factor: 3.0}},
        PostBuildHook: hook,
    })

    if 10 != provider.pool.openConnectionCount() || 2 != provider.pool.idleConnectionCount() {
        t.Fatalf("expected the configured pool, got %+v", provider.pool)
    }

    if time.Second != provider.timeout.connectTimeout || 2*time.Second != driverDuration(provider.timeout.readTimeout) {
        t.Fatalf("expected the configured deadlines, got %+v", provider.timeout)
    }

    if 4 != provider.retry.maxAttempts || 3.0 != provider.retry.backoff.Factor {
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

    expectPanicNamingField(t, "RetryConfig.Backoff.Factor", func() {
        NewProvider(ProviderConfig{Retry: RetryConfig{Backoff: bound.Backoff{Factor: math.NaN()}}})
    })
}

func TestMigrationProvider_KeepsHookRetryAndTransportOverTheMigrationShape(t *testing.T) {
    hookCalled := false
    explicitTlsConfig := &tls.Config{ServerName: "pinned.example.com"}

    provider := NewProvider(ProviderConfig{
        TlsConfig: explicitTlsConfig,
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            hookCalled = true
            return nil
        },
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(10 * time.Second)}},
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
    _ = derived.postBuildHook(testRuntime(), driver.NewConfig())
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

type postBuildHookContextKey struct{}

func TestProvider_ThePostBuildHookRunsUnderTheOpenersRuntime(t *testing.T) {
    hookErr := errors.New("hook stops the open")
    openerRuntime := testRuntimeUnder(context.WithValue(context.Background(), postBuildHookContextKey{}, "opener"))

    var seenRuntime containercontract.Runtime
    provider := newTestProvider(ProviderConfig{
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            seenRuntime = runtimeInstance

            return hookErr
        },
        Retry: singleAttemptRetry,
    })

    if _, openErr := provider.Open(openerRuntime, newTestParameters("db.internal", "3306", "melody", "melody_user", "melody_password"), nil); false == errors.Is(openErr, hookErr) {
        t.Fatalf("expected the hook's refusal to end the open, got %v", openErr)
    }

    if nil == seenRuntime {
        t.Fatal("expected the hook to run")
    }

    if openerRuntime.Scope() != seenRuntime.Scope() {
        t.Fatal("expected the hook handed the opener's scope")
    }

    if openerRuntime.Kind() != seenRuntime.Kind() {
        t.Fatalf("expected the hook handed the opener's kind %v, got %v", openerRuntime.Kind(), seenRuntime.Kind())
    }

    if "opener" != seenRuntime.Context().Value(postBuildHookContextKey{}) {
        t.Fatal("expected the hook's context derived from the opener's")
    }

    if nil == seenRuntime.Context().Err() {
        t.Fatal("expected the hook's runtime done once the open returned")
    }
}

func TestProvider_ARuntimeWithoutAScopeIsRefusedBeforeThePostBuildHookNamingTheArgument(t *testing.T) {
    hookRan := false
    provider := newTestProvider(ProviderConfig{
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            hookRan = true

            return nil
        },
        Retry: singleAttemptRetry,
    })

    runtimeInstance := scopelessRuntime{Runtime: testRuntime()}
    parameters := newTestParameters("db.internal", "3306", "melody", "melody_user", "melody_password")

    for door, open := range map[string]func() (*bun.DB, error){
        "mysql.Provider.Open":             func() (*bun.DB, error) { return provider.Open(runtimeInstance, parameters, nil) },
        "mysql.Provider.OpenForMigration": func() (*bun.DB, error) { return provider.OpenForMigration(runtimeInstance, parameters, nil) },
    } {
        _, openErr := open()
        if nil == openErr {
            t.Fatalf("%s: expected a runtime without a scope to be refused", door)
        }

        context := exception.LogContext(openErr, exception.LogContextOptions{})
        if "runtimeInstance" != context["argument"] || door != context["door"] || "ProviderConfig.PostBuildHook" != context["field"] {
            t.Fatalf("%s: expected the refusal to name the argument, the door and the hook's field, got %v: %v", door, context, openErr)
        }
    }

    if true == hookRan {
        t.Fatal("expected the hook not to run under a runtime without a scope")
    }
}

func TestProvider_ThePostBuildHooksContextEndsAtTheConnectTimeout(t *testing.T) {
    hookErr := errors.New("hook stops the open")
    connectTimeout := 3 * time.Second

    var seenDeadline time.Time
    var hasDeadline bool
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(connectTimeout)},
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            seenDeadline, hasDeadline = runtimeInstance.Context().Deadline()

            return hookErr
        },
        Retry: singleAttemptRetry,
    })

    started := time.Now()
    if _, openErr := provider.Open(testRuntime(), newTestParameters("db.internal", "3306", "melody", "melody_user", "melody_password"), nil); false == errors.Is(openErr, hookErr) {
        t.Fatalf("expected the hook's refusal to end the open, got %v", openErr)
    }

    if false == hasDeadline {
        t.Fatal("expected the hook's runtime to carry the connect timeout's deadline")
    }

    if seenDeadline.Before(started) || seenDeadline.After(time.Now().Add(connectTimeout)) {
        t.Fatalf("expected the deadline within the connect timeout, got %v from %v", seenDeadline.Sub(started), started)
    }
}

func TestProviderOpenBuildsDriverConfigAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConfig *driver.Config
    provider := newTestProvider(ProviderConfig{
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            seenConfig = driverConfig

            return hookErr
        },
        Retry: singleAttemptRetry,
    })

    database, openErr := provider.Open(
        testRuntime(),
        newTestParameters("db.internal", "3306", "melody", "melody_user", "melody_password"),
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
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(5 * time.Millisecond)}},
    })

    database, openErr := provider.Open(
        testRuntime(),
        newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
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
        testRuntime(),
        newTestParameters(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd),
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

func TestComputeBackoffDelayGrowsExponentiallyAndClampsAtTheMaxBackoff(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{MaxAttempts: bound.Of(3), Backoff: bound.Backoff{Initial: bound.Of(100 * time.Millisecond), Max: bound.Of(250 * time.Millisecond)}},
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

func TestProviderOpenForMigrationCancelsTheRetrySleep(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(2 * time.Second), Factor: 1.0}},
    })

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.OpenForMigration(
        testRuntimeUnder(ctx),
        newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
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

func TestProviderOpenCancelsTheRetrySleep(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(2 * time.Second), Factor: 1.0}},
    })

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    start := time.Now()
    database, openErr := provider.Open(
        testRuntimeUnder(ctx),
        newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
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
        newTestParameters("host", "3306", "database", "user", "password"),
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

    if "rewritten-host:5433" != connectionContext["dialedAddress"] {
        t.Fatalf("expected the dialled endpoint, got %v", connectionContext["dialedAddress"])
    }
}

func TestPortOrDefault_AnEmptyPortDialsTheDriversDefault(t *testing.T) {
    if "3306" != portOrDefault("") || "6000" != portOrDefault("6000") {
        t.Fatalf("expected the empty port read as 3306 and a set one kept, got %q / %q", portOrDefault(""), portOrDefault("6000"))
    }
}

func TestOpen_ACancelledContextReachesTheAttemptInFlight(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(10 * time.Second), ReadTimeout: bound.Of(10 * time.Second), WriteTimeout: bound.Of(10 * time.Second)},
        Retry:   singleAttemptRetry,
    })

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    started := time.Now()
    _, openErr := provider.Open(
        testRuntimeUnder(cancelledContext),
        newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"),
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

func (instance *capturingProviderLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *capturingProviderLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*capturingProviderLogger)(nil)

func TestOpenWithRetry_ACancelledOpenIsAWarningRatherThanANonTransientOutage(t *testing.T) {
    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(10 * time.Second), ReadTimeout: bound.Of(10 * time.Second), WriteTimeout: bound.Of(10 * time.Second)},
        Retry:   RetryConfig{},
    })

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := provider.Open(
        testRuntimeUnder(cancelledContext),
        newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"),
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

func TestOpenWithRetry_ACancellationDuringTheBackoffIsRecordedAndMarked(t *testing.T) {
    logger := &capturingProviderLogger{}

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(2 * time.Second), Factor: 1.0}},
    })

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    _, openErr := provider.Open(
        testRuntimeUnder(ctx),
        newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
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
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(2 * time.Millisecond)}},
    })

    _, openErr := provider.Open(
        testRuntimeUnder(context.Background()),
        newTestParameters("127.0.0.1", "1", "melody", "melody", "melody"),
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

func openObservingTheTlsPosture(t *testing.T, config ProviderConfig) *tls.Config {
    t.Helper()

    stopBeforeDial := errors.New("stop before the dial")

    var seenTlsConfig *tls.Config
    config.PostBuildHook = func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
        seenTlsConfig = driverConfig.TLS

        return stopBeforeDial
    }
    config.Retry = singleAttemptRetry

    provider := NewProvider(config)

    _, openErr := provider.Open(testRuntime(), newTestParameters("db.internal", "3306", "melody", "melody_user", "melody_password"), nil)
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
        Retry:   RetryConfig{MaxAttempts: bound.Of(1), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(5 * time.Millisecond)}},
    })

    database, openErr := provider.Open(
        testRuntime(),
        newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"),
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

func TestComputeBackoffDelayAnswersAConstantMultiplierInBoundedTime(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{Backoff: bound.Backoff{Initial: bound.Of(10 * time.Millisecond), Max: bound.Of(5 * time.Second), Factor: 1.0}},
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

func TestComputeBackoffDelayReadsAZeroAttemptAsTheFirst(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        Retry: RetryConfig{MaxAttempts: bound.Of(3), Backoff: bound.Backoff{Initial: bound.Of(100 * time.Millisecond), Max: bound.Of(250 * time.Millisecond)}},
    })

    if 100*time.Millisecond != provider.computeBackoffDelay(0) {
        t.Fatalf("expected a zero attempt to read as the first, got %s", provider.computeBackoffDelay(0))
    }
}

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

func TestProviderOpen_RefusesAnEmptyHostBeforeBuildingTheDriverConfig(t *testing.T) {
    provider := NewProvider(ProviderConfig{
        Insecure: true,
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            t.Fatal("the driver configuration was built for an empty host")

            return nil
        },
        Retry: singleAttemptRetry,
    })

    database, openErr := provider.Open(testRuntime(), newTestParameters("", "3306", "melody", "melody_user", "melody_password"), nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for an empty host")
    }

    if nil == openErr || false == strings.Contains(openErr.Error(), "the host is empty") {
        t.Fatalf("expected the refusal to name the host, got %v", openErr)
    }
}

func TestOpen_AnOutageIsFiledAsUnreachableAndARefusalIsNot(t *testing.T) {
    unreachable := newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    singleAttempt := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond)}, Retry: singleAttemptRetry})
    database, openErr := singleAttempt.Open(testRuntime(), unreachable, nil)
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

    retrying := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(100 * time.Millisecond)}, Retry: RetryConfig{MaxAttempts: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(time.Millisecond), Factor: 1}}})
    database, openErr = retrying.Open(testRuntime(), unreachable, nil)
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

    database, openErr = singleAttempt.Open(testRuntime(), newTestParameters("", "3306", "melody", "melody", "melody"), nil)
    if nil != database {
        _ = database.Close()
        t.Fatal("expected no database handle for an empty host")
    }

    if nil == openErr || true == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
        t.Fatalf("expected a refusal by name NOT filed as unreachable, got %v", openErr)
    }
}

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

func TestOpen_ACallersExpiredDeadlineIsNotAnUnreachableDatabase(t *testing.T) {
    expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: singleAttemptRetry}).Open(testRuntimeUnder(expired), newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"), nil)
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

func TestOpenWithRetry_ACallersExpiredDeadlineIsACleanStopWithoutARetry(t *testing.T) {
    logger := &capturingProviderLogger{}

    expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: RetryConfig{}}).Open(testRuntimeUnder(expired), newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"), logger)
    if nil == openErr || false == errors.Is(openErr, context.DeadlineExceeded) {
        t.Fatalf("expected the caller's deadline kept as the cause, got %v", openErr)
    }

    if 1 != len(logger.entries) || "database open cancelled by the caller's context" != logger.entries[0].message {
        t.Fatalf("expected one clean-stop warning and no retry, got %+v", logger.entries)
    }
}

func TestOpen_ALiveServerRefusalSpelledAroundAMarkerIsNotFiledAsUnreachable(t *testing.T) {
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

    for _, parameters := range []bunorm.ConnectionParameters{
        newTestParameters(host, port, "timeout", dsnConfig.User, dsnConfig.Passwd),
        newTestParameters(host, port, dsnConfig.DBName, "eof", "eof"),
    } {
        database, openErr := provider.Open(testRuntime(), parameters, nil)
        if nil != database {
            _ = database.Close()
            t.Fatalf("expected the refusal for %s/%s", parameters.Database, parameters.User)
        }

        var serverErr *driver.MySQLError
        if false == errors.As(openErr, &serverErr) {
            t.Fatalf("expected the server's own refusal, got %v", openErr)
        }

        if true == errors.Is(openErr, bunorm.ErrDatabaseUnreachable) {
            t.Fatalf("expected the refusal for %s/%s handed back by name, got it filed as unreachable: %v", parameters.Database, parameters.User, openErr)
        }
    }
}

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
        Retry:   RetryConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(2 * time.Millisecond)}},
    })

    if _, openErr := provider.Open(testRuntimeUnder(context.Background()), newTestParameters("127.0.0.1", "1", "melody", "melody", "melody"), logger); nil == openErr {
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

func TestProvider_OpenForMigrationReturnsInsideTheConnectTimeoutAgainstAPeerThatAcceptsAndStaysSilent(t *testing.T) {
    host, port := silentPeer(t)
    provider := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)}, Retry: singleAttemptRetry})
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    open := func() (*bun.DB, error) {
        return provider.OpenForMigration(testRuntimeUnder(ctx), newTestParameters(host, port, "melody", "melody", "melody"), nil)
    }

    elapsed, openErr := openAgainstASilentPeer(t, open)
    if nil == openErr {
        t.Fatal("expected the open against a silent peer to fail")
    }

    if 1500*time.Millisecond < elapsed {
        t.Fatalf("expected the open to return inside the 500ms connect timeout, it took %s", elapsed)
    }
}

func TestProvider_OpenReturnsInsideTheConnectTimeoutAgainstASilentPeer(t *testing.T) {
    host, port := silentPeer(t)
    provider := NewProvider(ProviderConfig{Insecure: true, Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), ReadTimeout: bound.Of(30 * time.Second), WriteTimeout: bound.Of(30 * time.Second)}, Retry: singleAttemptRetry})
    open := func() (*bun.DB, error) {
        return provider.Open(testRuntimeUnder(context.Background()), newTestParameters(host, port, "melody", "melody", "melody"), nil)
    }

    elapsed, openErr := openAgainstASilentPeer(t, open)
    if nil == openErr {
        t.Fatal("expected the open against a silent peer to fail")
    }

    if 1500*time.Millisecond < elapsed {
        t.Fatalf("expected the open to return inside the 500ms connect timeout, not the 30s read deadline, it took %s", elapsed)
    }
}

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

func TestConnectionTlsConfig_TheDefaultVerifiesAScopedLiteralWithoutItsZone(t *testing.T) {
    provider := NewProvider(ProviderConfig{Retry: singleAttemptRetry})

    if tlsConfig := provider.connectionTlsConfig("fe80::1%eth0"); nil == tlsConfig || "fe80::1" != tlsConfig.ServerName {
        t.Fatalf("expected the default config to verify fe80::1, got %+v", tlsConfig)
    }
}

func TestOpen_AnOpenHandedNoLoggerLeavesBunDiagnosticsWhereTheLastRoutingPutThem(t *testing.T) {
    t.Cleanup(bunorm.ResetDiagnostics)

    journal := &capturingProviderLogger{}
    bunorm.RouteDiagnostics(journal)

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond), ReadTimeout: bound.Of(time.Second), WriteTimeout: bound.Of(time.Second)},
        Retry:   singleAttemptRetry,
    })

    _, openErr := provider.Open(
        testRuntimeUnder(context.Background()),
        newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"),
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

func TestOpen_AnAlreadyCancelledOpenLeavesBunDiagnosticsWhereTheLastRoutingPutThem(t *testing.T) {
    t.Cleanup(bunorm.ResetDiagnostics)

    journal := &capturingProviderLogger{}
    bunorm.RouteDiagnostics(journal)

    handed := &capturingProviderLogger{}
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := newTestProvider(ProviderConfig{Retry: singleAttemptRetry}).Open(
        testRuntimeUnder(cancelledContext),
        newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"),
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
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            driverConfig.Net = network

            return nil
        },
    })

    _, openErr := provider.Open(testRuntimeUnder(context.Background()), newTestParameters("127.0.0.1", "1", "melody", "melody", "melody"), logger)
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

func TestOpen_AZeroIdleCapBootsOnThePingConnectionAndKeepsNoIdleConnectionAfter(t *testing.T) {
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
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            driverConfig.Net = network

            return nil
        },
    })

    database, openErr := provider.Open(testRuntime(), newTestParameters(host, port, dsnConfig.DBName, dsnConfig.User, dsnConfig.Passwd), nil)
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

func TestProvider_OpenReturnsOnACyclicChainFromAPostBuildHook(t *testing.T) {
    provider := newTestProvider(ProviderConfig{
        PostBuildHook: func(_ containercontract.Runtime, _ *driver.Config) error {
            return newCyclicChainError()
        },
        Retry: RetryConfig{MaxAttempts: bound.Of(1)},
    })

    assertReturnsOnACyclicChain(t, func() {
        if _, openErr := provider.Open(testRuntime(), newTestParameters("unused", "3306", "db", "user", "password"), nil); nil == openErr {
            t.Error("expected the hook's refusal")
        }
    })
}

func TestNewProvider_RefusesInsecureBesideTlsConfig(t *testing.T) {
    func() {
        defer func() {
            recovered := recover()
            if nil == recovered || false == strings.Contains(fmt.Sprint(recovered), "Insecure and TlsConfig") {
                t.Fatalf("expected Insecure beside TlsConfig refused naming both, got %v", recovered)
            }
        }()

        _ = NewProvider(ProviderConfig{Insecure: true, TlsConfig: &tls.Config{}})
    }()

    _ = NewProvider(ProviderConfig{Insecure: true})
    _ = NewProvider(ProviderConfig{TlsConfig: &tls.Config{}})
}

type nilContextRuntime struct {
    containercontract.Runtime
}

func (instance nilContextRuntime) Context() context.Context {
    return nil
}

func TestProvider_BothDoorsRefuseARuntimeThatCarriesNoContext(t *testing.T) {
    t.Cleanup(bunorm.ResetDiagnostics)

    provider := NewProvider(ProviderConfig{Insecure: true, Retry: singleAttemptRetry})

    var typedNil *nilRuntimeImplementation

    doors := map[string]func(runtimeInstance containercontract.Runtime, logger *capturingProviderLogger) (*bun.DB, error){
        "Open": func(runtimeInstance containercontract.Runtime, logger *capturingProviderLogger) (*bun.DB, error) {
            return provider.Open(runtimeInstance, newTestParameters("127.0.0.1", "1", "melody", "melody", "melody"), logger)
        },
        "OpenForMigration": func(runtimeInstance containercontract.Runtime, logger *capturingProviderLogger) (*bun.DB, error) {
            return provider.OpenForMigration(runtimeInstance, newTestParameters("127.0.0.1", "1", "melody", "melody", "melody"), logger)
        },
    }

    runtimes := map[string]containercontract.Runtime{
        "nil":        nil,
        "typed nil":  typedNil,
        "no context": nilContextRuntime{Runtime: testRuntime()},
    }

    for doorName, door := range doors {
        for runtimeName, runtimeInstance := range runtimes {
            bunorm.ResetDiagnostics()

            logger := &capturingProviderLogger{}
            database, openErr := door(runtimeInstance, logger)
            if nil != database {
                t.Fatalf("%s under %s: expected no database", doorName, runtimeName)
            }

            if nil == openErr || false == strings.Contains(openErr.Error(), "the runtime is nil or carries no context") {
                t.Fatalf("%s under %s: expected the runtime refusal, got %v", doorName, runtimeName, openErr)
            }

            _ = schema.SafeQuery("SELECT 1", []any{42})

            if 0 != len(logger.entries) {
                t.Fatalf("%s under %s: expected nothing routed nor journaled, got %v", doorName, runtimeName, logger.entries)
            }
        }
    }
}

type nilRuntimeImplementation struct {
    containercontract.Runtime
}

func TestOpenForMigration_TheDriverConfigReceivesTheConnectTimeoutAndTheLiftedDeadlines(t *testing.T) {
    var seen *driver.Config

    provider := NewProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(7 * time.Second), ReadTimeout: bound.Of(11 * time.Second), WriteTimeout: bound.Of(13 * time.Second)},
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            seen = driverConfig

            return errors.New("stop before the connection")
        },
        Retry: singleAttemptRetry,
    })

    if _, openErr := provider.OpenForMigration(testRuntime(), newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"), &capturingProviderLogger{}); nil == openErr {
        t.Fatal("expected the hook's refusal to end the open")
    }

    if nil == seen || 7*time.Second != seen.Timeout || 0 != seen.ReadTimeout || 0 != seen.WriteTimeout {
        t.Fatalf("expected the 7s connect timeout and no read or write deadline on the migration connection, got %+v", seen)
    }
}

func TestOpen_ADoneContextIsRefusedBeforeTheHookRuns(t *testing.T) {
    hookCalls := 0

    provider := NewProvider(ProviderConfig{
        PostBuildHook: func(runtimeInstance containercontract.Runtime, driverConfig *driver.Config) error {
            hookCalls++

            return nil
        },
        Retry: singleAttemptRetry,
    })

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := provider.Open(testRuntimeUnder(doneContext), newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"), &capturingProviderLogger{})
    if nil == openErr || false == strings.Contains(openErr.Error(), "database open cancelled before the attempt") || false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the refusal before the attempt carrying the cancellation, got %v", openErr)
    }

    if 0 != hookCalls {
        t.Fatalf("expected the hook never run, got %d calls", hookCalls)
    }
}

func TestOpen_TheCancellationRefusalsCarryTheCallersCause(t *testing.T) {
    callerCause := errors.New("the client hung up")

    doneContext, cancelDone := context.WithCancelCause(context.Background())
    cancelDone(callerCause)

    _, refusedErr := NewProvider(ProviderConfig{Retry: singleAttemptRetry}).Open(testRuntimeUnder(doneContext), newTestParameters("203.0.113.1", "3306", "melody", "melody", "melody"), &capturingProviderLogger{})
    if false == errors.Is(refusedErr, callerCause) || false == errors.Is(refusedErr, context.Canceled) {
        t.Fatalf("expected the refusal before the attempt to carry the cause and the cancellation, got %v", refusedErr)
    }

    provider := newTestProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(50 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(5), Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(2 * time.Second), Factor: 1.0}},
    })

    backoffContext, cancelBackoff := context.WithCancelCause(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancelBackoff(callerCause)
    }()

    _, backoffErr := provider.Open(testRuntimeUnder(backoffContext), newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody"), &capturingProviderLogger{})
    if false == strings.Contains(fmt.Sprintf("%v", backoffErr), "retry cancelled") || false == errors.Is(backoffErr, callerCause) || false == errors.Is(backoffErr, context.Canceled) {
        t.Fatalf("expected the backoff refusal to carry the cause and the cancellation, got %v", backoffErr)
    }
}

func TestProvider_OpenForMigrationNamesItselfWhenHandedNoRuntime(t *testing.T) {
    _, openErr := NewProvider(ProviderConfig{}).OpenForMigration(nil, bunorm.ConnectionParameters{}, nil)
    if "mysql.Provider.OpenForMigration" != exception.LogContext(openErr, exception.LogContextOptions{})["door"] {
        t.Fatalf("expected the refusal to name the migration door, got %v", openErr)
    }
}

func TestOptionalDependencies_NilIsTheDefaultAndATypedNilIsRefused(t *testing.T) {
    provider := NewProvider(ProviderConfig{Retry: RetryConfig{MaxAttempts: bound.Of(1)}})
    parameters := newTestParameters("127.0.0.1", "1", "melody_unreachable", "melody", "melody")

    captureStandardErrorOf(t, func() {
        database, openErr := provider.Open(testRuntime(), parameters, nil)
        if nil != database {
            _ = database.Close()
        }

        var refusal *exception.Error
        if true == errors.As(openErr, &refusal) && "mysql provider logger is a typed nil" == refusal.Message() {
            t.Fatalf("expected an absent logger to reach the dial, got %v", openErr)
        }
    })

    database, openErr := provider.Open(testRuntime(), parameters, (*capturingProviderLogger)(nil))
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database for a typed-nil logger")
    }

    var refusal *exception.Error
    if false == errors.As(openErr, &refusal) || "mysql provider logger is a typed nil" != refusal.Message() || "mysql.Provider.Open.logger" != refusal.Context()["field"] {
        t.Fatalf("expected the typed-nil refusal naming mysql.Provider.Open.logger, got %v", openErr)
    }
}

func captureStandardErrorOf(t *testing.T, action func()) {
    t.Helper()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    savedStandardError := os.Stderr
    logging.CloseEmergencyLogger()
    os.Stderr = writeEnd
    _ = logging.EmergencyLogger()
    os.Stderr = savedStandardError

    action()

    logging.CloseEmergencyLogger()
    _ = writeEnd.Close()
    _, _ = io.ReadAll(readEnd)
    _ = readEnd.Close()
}
