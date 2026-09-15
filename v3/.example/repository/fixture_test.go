package repository

import (
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
    "database/sql"
    "testing"
    "time"
)

type sqlStateError struct {
    state string
}

func (instance *sqlStateError) Error() string {
    return "some server error"
}

func (instance *sqlStateError) SQLState() string {
    return instance.state
}

type stubResult struct {
    affected    int64
    affectedErr error
}

func (instance stubResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance stubResult) RowsAffected() (int64, error) {
    return instance.affected, instance.affectedErr
}

var _ sql.Result = stubResult{}

func renderedUserByUsernameQuery(t *testing.T, wanted string) string {
    t.Helper()

    repositoryInstance := &bunUserRepository{database: newRenderingDatabase()}

    return repositoryInstance.userByUsernameQuery(&userRow{}, wanted).String()
}

func renderedUsernameTakenByAnotherQuery(t *testing.T, wanted string, excludedId string) string {
    t.Helper()

    repositoryInstance := &bunUserRepository{database: newRenderingDatabase()}

    return repositoryInstance.usernameTakenByAnotherQuery(wanted, excludedId).String()
}

type renderingDialect struct {
    schema.BaseDialect

    tables *schema.Tables
}

func newRenderingDialect() *renderingDialect {
    instance := &renderingDialect{}
    instance.tables = schema.NewTables(instance)

    return instance
}

func (instance *renderingDialect) Init(database *sql.DB) {
}

func (instance *renderingDialect) Name() dialect.Name {
    return dialect.SQLite
}

func (instance *renderingDialect) Features() feature.Feature {
    return 0
}

func (instance *renderingDialect) Tables() *schema.Tables {
    return instance.tables
}

func (instance *renderingDialect) OnTable(table *schema.Table) {
}

func (instance *renderingDialect) IdentQuote() byte {
    return '`'
}

func (instance *renderingDialect) AppendSequence(payload []byte, table *schema.Table, field *schema.Field) []byte {
    return payload
}

func (instance *renderingDialect) DefaultVarcharLen() int {
    return 0
}

func (instance *renderingDialect) DefaultSchema() string {
    return "main"
}

func newRenderingDatabase() *bun.DB {
    return bun.NewDB(nil, newRenderingDialect())
}

var _ schema.Dialect = (*renderingDialect)(nil)

var currencyProbeQuoteInstant = time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC)

const concurrentRounds = 500
