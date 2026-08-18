package main

import (
    "context"
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

/* the tables the v3 .example application owns. They are named here, once, because more than one section reads
or cleans them and a second spelling of a table name is a section that quietly asserts nothing. */
const (
    exampleV3ProductTable = "melody_example_v3_product"
    exampleV3UserTable    = "melody_example_v3_user"
    exampleV3JournalTable = "melody_example_v3_catalog_journal"
    exampleV3AuditTable   = "melody_example_v3_audit"
)

/* removeExampleV3ProductProbes removes probe products AND everything the application recorded about them.

Removing only the products is not enough, and the reason is not tidiness. The example hands out the next free
identifier, so a deleted prod-6 is handed out again to the next probe — and a later section that reads "the trail
of prod-6" would then find the previous occupant's entries and assert against a history that is not its own. A
probe's records have to leave with it or the identifier carries them into the next run. */
func removeExampleV3ProductProbes(label string, database *bun.DB, names []string) {
    if false == exampleTableExists(label, database, exampleV3ProductTable) {
        return
    }

    productIds := make([]string, 0, len(names))

    selectErr := database.
        NewSelect().
        ColumnExpr("id").
        Table(exampleV3ProductTable).
        Where("name IN (?)", bun.In(names)).
        Scan(context.Background(), &productIds)
    if nil != selectErr {
        fail("%s: look up the probe products for removal: %v", label, selectErr)
    }

    for _, productId := range productIds {
        removeExampleV3ProductRecords(label, database, productId)
    }

    _, productDeleteErr := database.
        NewDelete().
        Table(exampleV3ProductTable).
        Where("name IN (?)", bun.In(names)).
        Exec(context.Background())
    if nil != productDeleteErr {
        fail("%s: remove the probe products: %v", label, productDeleteErr)
    }
}

/* removeExampleV3ProductRecords removes the journal entries and the audit trail one product left behind. */
func removeExampleV3ProductRecords(label string, database *bun.DB, productId string) {
    if false == exampleTableExists(label, database, exampleV3JournalTable) {
        return
    }

    _, journalDeleteErr := database.
        NewDelete().
        Table(exampleV3JournalTable).
        Where("subject = ?", "product").
        Where("subject_id = ?", productId).
        Exec(context.Background())
    if nil != journalDeleteErr {
        fail("%s: remove the journal entries of %s: %v", label, productId, journalDeleteErr)
    }

    removeExampleV3AuditTrail(label, database, "product", productId)
}

func removeExampleV3AuditTrail(label string, database *bun.DB, entity string, entityId string) {
    if false == exampleTableExists(label, database, exampleV3AuditTable) {
        return
    }

    _, deleteErr := database.
        NewDelete().
        Table(exampleV3AuditTable).
        Where("entity = ?", entity).
        Where("entity_id = ?", entityId).
        Exec(context.Background())
    if nil != deleteErr {
        fail("%s: remove the trail of %s %s: %v", label, entity, entityId, deleteErr)
    }
}

/* exampleTableExists answers whether a table is there yet.

The example applications create their tables on the path that first reaches them, so on a database that has just
been created — which is what `./dc up:all` gives after a volume is recreated — a major nobody has sent a request
to yet has none. A section that cleans up before it starts must not fail on that: a table that does not exist
holds no leftovers from a previous run, which is exactly the state the cleanup is trying to reach. */
func exampleTableExists(label string, database *bun.DB, tableName string) bool {
    count := 0

    scanErr := database.
        NewSelect().
        ColumnExpr("COUNT(*)").
        TableExpr("information_schema.tables").
        Where("table_schema = DATABASE()").
        Where("table_name = ?", tableName).
        Scan(context.Background(), &count)
    if nil != scanErr {
        fail("%s: ask whether %s exists: %v", label, tableName, scanErr)
    }

    return 0 < count
}
