package pgsql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v2"
)

/* @info openWithRetry must tolerate a nil logger via EnsureLogger instead of nil-dereferencing on the info/warning path (CR #65 back-port of the v1/v3 guard) */

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := NewProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(100 * time.Millisecond)).
        WithRetryConfig(NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0))

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
