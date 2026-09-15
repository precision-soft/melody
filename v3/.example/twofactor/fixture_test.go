package twofactor

import (
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    "github.com/uptrace/bun/schema"
    "database/sql"
    "testing"
    "time"
)

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
    return dialect.MySQL
}

func (instance *renderingDialect) Features() feature.Feature {
    return feature.InsertOnDuplicateKey
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

func renderedEnrollmentUpsert(t *testing.T) string {
    t.Helper()

    store := &Store{database: newRenderingDatabase()}

    return store.enrollmentUpsert(&Enrollment{
        UserIdentifier: "user-2",
        Secret:         melodyencrypt.EncryptedString("zz-secret"),
        RecoveryCodes:  melodyencrypt.EncryptedString(`["zz-one"]`),
        CreatedAt:      time.Unix(1, 0),
    }).String()
}
