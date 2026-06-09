package mysql_test

import (
    "net"
    "os"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v3"
    mysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
)

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

    provider := mysql.NewProvider(
        mysql.WithTimeoutConfig(
            mysql.NewTimeoutConfig(0, 30*time.Second, 30*time.Second),
        ),
    )

    database, openErr := provider.Open(params, nil)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}
