package pgsql_test

import (
    "os"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    pgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
)

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

    provider := pgsql.NewProvider(
        pgsql.WithInsecure(true),
        pgsql.WithTimeoutConfig(pgsql.NewTimeoutConfig(0)),
    )

    database, openErr := provider.Open(params, nil)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}
