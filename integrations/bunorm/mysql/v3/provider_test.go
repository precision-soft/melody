package mysql

import (
    "errors"
    "math"
    "net"
    "os"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v3"
)

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := NewProvider(
        WithTimeoutConfig(NewTimeoutConfig(100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond)),
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0)),
    )

    params := bunorm.ConnectionParams{
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

func TestComputeBackoffDelayDegenerateValuesFallBackToDefaults(t *testing.T) {
    provider := NewProvider(
        WithRetryConfig(NewRetryConfig(3, -time.Second, -time.Second, 0.5)),
    )

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

func TestProviderOpenWithZeroConnectTimeoutConnects(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql provider integration test")
    }

    config, parseErr := driver.ParseDSN(dsn)
    if nil != parseErr {
        t.Fatalf("parse dsn: %v", parseErr)
    }

    host, port, splitErr := net.SplitHostPort(config.Addr)
    if nil != splitErr {
        t.Skipf("MYSQL_DSN address %q is not host:port; skipping", config.Addr)
    }

    params := bunorm.ConnectionParams{
        Host:     host,
        Port:     port,
        Database: config.DBName,
        User:     config.User,
        Password: config.Passwd,
    }

    provider := NewProvider(
        WithTimeoutConfig(
            NewTimeoutConfig(0, 30*time.Second, 30*time.Second),
        ),
    )

    database, openErr := provider.Open(params, nil)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}

/* @info NaN fails every comparison, so a NaN multiplier would slip through a `1 > x` clamp, poison the float-space growth and convert to a negative duration — an immediate re-dial storm; the not-at-least-1 clamp resolves it to the default. */
func TestComputeBackoffDelayNaNMultiplierFallsBackToDefault(t *testing.T) {
    provider := NewProvider(
        WithRetryConfig(NewRetryConfig(3, -time.Second, -time.Second, math.NaN())),
    )

    if 1*time.Second != provider.computeBackoffDelay(2) {
        t.Fatalf("expected a NaN multiplier to fall back to the default 2.0, got %s", provider.computeBackoffDelay(2))
    }

    if 5*time.Second != provider.computeBackoffDelay(10) {
        t.Fatalf("expected the NaN fallback to keep the default 5s clamp, got %s", provider.computeBackoffDelay(10))
    }
}

/* @info a restarting server answers with 1053 before closing; treating it as transient keeps the cold-start retry loop alive instead of failing the open on the first attempt */
func TestIsTransientErrorRecognizesServerShutdownInProgress(t *testing.T) {
    provider := NewProvider()

    if false == provider.isTransientError(errors.New("Error 1053: Server shutdown in progress")) {
        t.Fatalf("expected a shutdown-in-progress error to be transient")
    }

    if true == provider.isTransientError(errors.New("Error 1064: You have an error in your SQL syntax")) {
        t.Fatalf("expected a syntax error to stay non-transient")
    }
}

/* @info a connection abort is the one retryable condition the deprecated net.Error.Temporary() uniquely covered; it is recognised through an explicit marker instead */
func TestIsTransientErrorRecognizesConnectionAbort(t *testing.T) {
    provider := NewProvider()

    if false == provider.isTransientError(errors.New("write tcp 10.0.0.1:3306: software caused connection abort")) {
        t.Fatalf("expected a connection abort to be transient")
    }

    /* the same errno carries a different text on windows, where the deprecated call used to cover it too */
    if false == provider.isTransientError(errors.New("write tcp 10.0.0.1:3306: An established connection was aborted by the software in your host machine.")) {
        t.Fatalf("expected the windows spelling of a connection abort to be transient")
    }
}
