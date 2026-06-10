package mysql_test

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    mysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
)

func TestProviderOpenWithRetryAndNilLoggerDoesNotPanic(t *testing.T) {
    provider := mysql.NewProvider(
        mysql.WithTimeoutConfig(mysql.NewTimeoutConfig(100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond)),
        mysql.WithRetryConfig(mysql.NewRetryConfig(2, time.Millisecond, 5*time.Millisecond, 2.0)),
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
