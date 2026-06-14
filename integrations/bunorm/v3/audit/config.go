package audit

import (
    "context"
    "regexp"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

var auditTableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateAuditTableName(table string) {
    if false == auditTableNamePattern.MatchString(table) {
        exception.Panic(exception.NewError("audit table name is not a valid identifier", map[string]any{"table": table}, nil))
    }
}

type EntityOptions struct {
    Table         string
    IgnoredFields []string
}

type Registry struct {
    defaultTable        string
    globalIgnoredFields []string
    optionsByEntity     map[string]EntityOptions
}

func NewRegistry(defaultTable string, globalIgnoredFields ...string) *Registry {
    if "" == defaultTable {
        defaultTable = DefaultTable
    }

    validateAuditTableName(defaultTable)

    return &Registry{
        defaultTable:        defaultTable,
        globalIgnoredFields: globalIgnoredFields,
        optionsByEntity:     make(map[string]EntityOptions),
    }
}

func (instance *Registry) Register(entity string, options EntityOptions) *Registry {
    if "" != options.Table {
        validateAuditTableName(options.Table)
    }

    instance.optionsByEntity[entity] = options

    return instance
}

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
