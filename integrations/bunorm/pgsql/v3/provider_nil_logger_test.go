package pgsql_test

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    pgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
)

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := pgsql.NewProvider(
        pgsql.WithInsecure(true),
        pgsql.WithTimeoutConfig(pgsql.NewTimeoutConfig(100*time.Millisecond)),
        pgsql.WithRetryConfig(pgsql.NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0)),
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
