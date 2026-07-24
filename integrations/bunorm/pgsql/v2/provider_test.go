package pgsql

import (
    "context"
    "errors"
    "math"
    "net"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v2"
    "github.com/uptrace/bun/driver/pgdriver"
)

/* @info openWithRetry must tolerate a nil logger via EnsureLogger instead of nil-dereferencing on the info/warning path */

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := NewProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(100 * time.Millisecond)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0))

    params := bunorm.ConnectionParameters{
        Host:     "127.0.0.1",
        Port:     "1",
        Database: "melody_unreachable",
        User:     "melody",
        Password: "melody",
    }

    database, openErr := provider.Open(params, nil)
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle for an unreachable host")
    }

    if nil == openErr {
        t.Fatalf("expected a connection error for an unreachable host")
    }
}

func TestProviderBuilderMethodsSetConfigs(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)
    timeoutConfig := NewTimeoutConfig(time.Second)
    retryConfig := NewRetryConfig(4, time.Millisecond, time.Second, 3.0)

    provider := NewProvider().
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

/* @info Open builds the connector from the connection params and aborts on a post-build hook error before dialing */

func TestProviderOpenBuildsConnectorAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConnector *pgdriver.Connector
    provider := NewProvider(
        WithInsecure(true),
        WithPostBuildHook(func(ctx context.Context, connector *pgdriver.Connector) error {
            seenConnector = connector

            return hookErr
        }),
    )

    params := bunorm.ConnectionParameters{
        Host:     "db.internal",
        Port:     "5432",
        Database: "melody",
        User:     "melody_user",
        Password: "melody_password",
    }

    database, openErr := provider.Open(params, nil)
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

func TestProviderOpenWithZeroConnectTimeoutConnects(t *testing.T) {
    host := os.Getenv("PGSQL_HOST")
    if "" == host {
        t.Skip("PGSQL_HOST not set; skipping pgsql provider integration test")
    }

    params := bunorm.ConnectionParameters{
        Host:     host,
        Port:     os.Getenv("PGSQL_PORT"),
        Database: os.Getenv("PGSQL_DATABASE"),
        User:     os.Getenv("PGSQL_USER"),
        Password: os.Getenv("PGSQL_PASSWORD"),
    }

    provider := NewProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(0))

    database, openErr := provider.Open(params, nil)
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
    provider := NewProvider().
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
    provider := NewProvider().
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
    provider := NewProvider().
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
    provider := NewProvider()

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
    provider := NewProvider()

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
    provider := NewProvider().
        WithRetryConfig(NewRetryConfig(3, -time.Second, -time.Second, math.NaN()))

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a NaN multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the NaN fallback to keep the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}
