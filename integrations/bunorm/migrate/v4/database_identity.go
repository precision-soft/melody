package migrate

import (
    "context"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
)

type databaseIdentity struct {
    CurrentDatabase *string `bun:"currentDatabase"`
    Hostname        string  `bun:"hostname"`
    Port            uint16  `bun:"port"`
    CurrentUser     string  `bun:"currentUser"`
    Version         string  `bun:"version"`
}

func fetchDatabaseIdentity(ctx context.Context, db *bun.DB) (*databaseIdentity, error) {
    dialectName := db.Dialect().Name()

    if dialect.MySQL == dialectName {
        return fetchMysqlDatabaseIdentity(ctx, db)
    }

    if dialect.PG == dialectName {
        return fetchPgsqlDatabaseIdentity(ctx, db)
    }

    return nil, nil
}

func fetchMysqlDatabaseIdentity(ctx context.Context, db *bun.DB) (*databaseIdentity, error) {
    var currentDatabase *string
    if scanErr := db.NewSelect().ColumnExpr("DATABASE()").Scan(ctx, &currentDatabase); nil != scanErr {
        return nil, scanErr
    }

    var hostname string
    var port uint16
    var currentUser string
    if scanErr := db.NewSelect().
        ColumnExpr("@@hostname").
        ColumnExpr("@@port").
        ColumnExpr("CURRENT_USER()").
        Scan(ctx, &hostname, &port, &currentUser); nil != scanErr {
        return nil, scanErr
    }

    var version string
    if scanErr := db.NewSelect().ColumnExpr("VERSION()").Scan(ctx, &version); nil != scanErr {
        return nil, scanErr
    }

    return &databaseIdentity{
        CurrentDatabase: currentDatabase,
        Hostname:        hostname,
        Port:            port,
        CurrentUser:     currentUser,
        Version:         version,
    }, nil
}

/* fetchPgsqlDatabaseIdentity answers the same block for PostgreSQL. The server address is read as text because inet_server_addr returns an inet value and is NULL over a unix socket — the very connection a local migration is most likely to use — so a typed scan would refuse where the block matters most. */
func fetchPgsqlDatabaseIdentity(ctx context.Context, db *bun.DB) (*databaseIdentity, error) {
    var currentDatabase *string
    var hostname *string
    var port *uint16
    var currentUser string
    if scanErr := db.NewSelect().
        ColumnExpr("current_database()").
        ColumnExpr("host(inet_server_addr())").
        ColumnExpr("inet_server_port()").
        ColumnExpr("current_user").
        Scan(ctx, &currentDatabase, &hostname, &port, &currentUser); nil != scanErr {
        return nil, scanErr
    }

    var version string
    if scanErr := db.NewSelect().ColumnExpr("version()").Scan(ctx, &version); nil != scanErr {
        return nil, scanErr
    }

    hostnameString := "<local socket>"
    if nil != hostname {
        hostnameString = *hostname
    }

    portNumber := uint16(0)
    if nil != port {
        portNumber = *port
    }

    return &databaseIdentity{
        CurrentDatabase: currentDatabase,
        Hostname:        hostnameString,
        Port:            portNumber,
        CurrentUser:     currentUser,
        Version:         version,
    }, nil
}
