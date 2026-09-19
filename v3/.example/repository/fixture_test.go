package repository

import (
    "database/sql"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/dialect/feature"
    "github.com/uptrace/bun/schema"
)

/* renderingDialect is the least a bun handle needs in order to RENDER a statement: the mysql dialect asks
   the connection for its version as it is installed, so a handle built on it cannot be made without a
   database, while what these tests read is the statement bun composes, not the answer a server would give.
   Nothing here executes, so the handle carries no driver at all. The migration package keeps a fuller twin
   of this shape, which also records what was executed. */
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

/* newRenderingDatabase answers a handle that can compose a statement and cannot run one. */
func newRenderingDatabase() *bun.DB {
    return bun.NewDB(nil, newRenderingDialect())
}

var _ schema.Dialect = (*renderingDialect)(nil)
