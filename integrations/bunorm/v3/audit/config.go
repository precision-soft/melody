package audit

import (
    "context"

    "github.com/uptrace/bun"
)

/**
 * EntityOptions configures how one entity is audited. Table routes its entries to a dedicated
 * audit table (per-entity tables, like the PHP reference); empty means the recorder's default table.
 * IgnoredFields are dropped from the change-set in addition to the struct-tag rules in change.go.
 */
type EntityOptions struct {
    Table         string
    IgnoredFields []string
}

/**
 * Registry maps audited entities to their options and global ignored fields. It resolves the
 * target table and the effective ignore-set per entity, and can materialise the per-entity tables.
 */
type Registry struct {
    defaultTable        string
    globalIgnoredFields []string
    optionsByEntity     map[string]EntityOptions
}

func NewRegistry(defaultTable string, globalIgnoredFields ...string) *Registry {
    if "" == defaultTable {
        defaultTable = DefaultTable
    }

    return &Registry{
        defaultTable:        defaultTable,
        globalIgnoredFields: globalIgnoredFields,
        optionsByEntity:     make(map[string]EntityOptions),
    }
}

func (instance *Registry) Register(entity string, options EntityOptions) *Registry {
    instance.optionsByEntity[entity] = options

    return instance
}

func (instance *Registry) tableFor(entity string) string {
    if options, exists := instance.optionsByEntity[entity]; true == exists && "" != options.Table {
        return options.Table
    }

    return instance.defaultTable
}

func (instance *Registry) ignoredFieldsFor(entity string) map[string]struct{} {
    ignored := make(map[string]struct{}, len(instance.globalIgnoredFields))
    for _, field := range instance.globalIgnoredFields {
        ignored[field] = struct{}{}
    }

    if options, exists := instance.optionsByEntity[entity]; true == exists {
        for _, field := range options.IgnoredFields {
            ignored[field] = struct{}{}
        }
    }

    return ignored
}

/** distinctTables returns the default table plus every per-entity table, deduplicated. */
func (instance *Registry) distinctTables() []string {
    seen := map[string]struct{}{instance.defaultTable: {}}
    tables := []string{instance.defaultTable}

    for _, options := range instance.optionsByEntity {
        if "" == options.Table {
            continue
        }
        if _, exists := seen[options.Table]; true == exists {
            continue
        }
        seen[options.Table] = struct{}{}
        tables = append(tables, options.Table)
    }

    return tables
}

/**
 * EnsureSchema creates the audit and audit-transaction tables (if absent) for every distinct table
 * the registry routes to. Run once at boot/migration time.
 */
func (instance *Registry) EnsureSchema(ctx context.Context, database *bun.DB) error {
    if _, txErr := database.NewCreateTable().Model((*Transaction)(nil)).IfNotExists().Exec(ctx); nil != txErr {
        return txErr
    }

    for _, table := range instance.distinctTables() {
        if _, createErr := database.NewCreateTable().Model((*Entry)(nil)).ModelTableExpr(table).IfNotExists().Exec(ctx); nil != createErr {
            return createErr
        }
    }

    return nil
}
