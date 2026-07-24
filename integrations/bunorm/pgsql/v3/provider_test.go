package pgsql

import (
    "errors"
    "math"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
)

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := NewProvider(
        WithInsecure(true),
        WithTimeoutConfig(NewTimeoutConfig(100*time.Millisecond)),
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0)),
    )

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

    provider := NewProvider(
        WithInsecure(true),
        WithTimeoutConfig(NewTimeoutConfig(0)),
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

/* @info a server replaying WAL accepts TCP but answers with the cold-start FATALs; treating them as transient keeps the retry loop alive instead of failing the open on the first attempt */
func TestIsTransientErrorRecognizesColdStartFatals(t *testing.T) {
    provider := NewProvider()

    coldStartErrors := []string{
        "FATAL: the database system is starting up (SQLSTATE=57P03)",
        "FATAL: the database system is in recovery mode (SQLSTATE=57P03)",
        "FATAL: the database system is shutting down (SQLSTATE=57P03)",
    }

    for _, message := range coldStartErrors {
        if false == provider.isTransientError(errors.New(message)) {
            t.Fatalf("expected %q to be transient", message)
        }
    }

    if true == provider.isTransientError(errors.New("FATAL: password authentication failed for user")) {
        t.Fatalf("expected an authentication failure to stay non-transient")
    }
}

/* @info a connection abort is the one retryable condition the deprecated net.Error.Temporary() uniquely covered; it is recognised through an explicit marker instead */
func TestIsTransientErrorRecognizesConnectionAbort(t *testing.T) {
    provider := NewProvider()

    if false == provider.isTransientError(errors.New("write tcp 10.0.0.1:5432: software caused connection abort")) {
        t.Fatalf("expected a connection abort to be transient")
    }

    /* the same errno carries a different text on windows, where the deprecated call used to cover it too */
    if false == provider.isTransientError(errors.New("write tcp 10.0.0.1:5432: An established connection was aborted by the software in your host machine.")) {
        t.Fatalf("expected the windows spelling of a connection abort to be transient")
    }
}
