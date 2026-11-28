package twofactor

import (
    "database/sql"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
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
    return dialect.SQLite
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

type moduleRegistrar struct {
    melodycontainercontract.Container
}

func (instance moduleRegistrar) RegisterService(serviceName string, provider any, options melodycontainercontract.RegisterOptions) {
    instance.MustRegister(serviceName, provider, options)
}
