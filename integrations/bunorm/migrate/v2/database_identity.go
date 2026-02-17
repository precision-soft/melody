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
