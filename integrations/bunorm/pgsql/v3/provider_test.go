package pgsql

import (
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
    host := os.Getenv("PGSQL_HOST")
    if "" == host {
        t.Skip("PGSQL_HOST not set; skipping pgsql provider integration test")
    }

    params := bunorm.ConnectionParams{
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

/** @info NaN fails every comparison, so a NaN multiplier would slip through a `1 > x` clamp, poison the float-space growth and convert to a negative duration — an immediate re-dial storm; the not-at-least-1 clamp resolves it to the default. */
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
