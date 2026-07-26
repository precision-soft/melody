package main

import (
    "database/sql"
    "net"

    mysqldriver "github.com/go-sql-driver/mysql"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

/* openPostgres opens a bun.DB over the pgdriver for the given DSN, the same shape the outbox and pgsql
integration tests use. */
func openPostgres(dsn string) *bun.DB {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))

    return bun.NewDB(sqldb, pgdialect.New())
}

/* openMysql opens the harness's OWN connection to the database the example writes through. It goes through the
bunorm mysql integration's provider rather than a bare sql.Open so the harness reaches the store exactly the way
the framework does — same driver, same dialect, same connection handling — while still being an independent client
of it: the whole point of the out-of-band reads is that they do not trust the application's own read path.

The DSN is parsed with the driver's own parser and handed to the provider as connection parameters, because the
provider's contract is host/port/user/password/database while the harness's env contract is a single DSN (the same
shape POSTGRES_DSN already uses). */
func openMysql(label string, dsn string) *bun.DB {
    config, parseErr := mysqldriver.ParseDSN(dsn)
    if nil != parseErr {
        fail("%s: MYSQL_DSN is not a valid mysql dsn (%v)", label, parseErr)
    }

    host, port := splitMysqlAddress(config.Addr)

    database, openErr := melodymysql.NewProvider().Open(
        melodybunorm.ConnectionParameters{
            Host:     host,
            Port:     port,
            Database: config.DBName,
            User:     config.User,
            Password: config.Passwd,
        },
        nil,
    )
    if nil != openErr {
        fail("%s: open the harness's own mysql connection: %v", label, openErr)
    }

    return database
}

/* splitMysqlAddress falls back to the default port rather than failing on a portless address: the driver's parser
already accepts "host" on its own, so refusing it here would reject a DSN mysql itself considers valid. */
func splitMysqlAddress(address string) (string, string) {
    host, port, splitErr := net.SplitHostPort(address)
    if nil != splitErr {
        return address, "3306"
    }

    return host, port
}
