package audit

import (
    "context"
    "regexp"
    "sort"
    "sync"

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

    /* CaptureDeleteBeforeImage makes an audited delete load and lock the row first so the trail carries its field values. It costs a select and a row lock on every delete of this entity and turns deleting an absent row into an error, so it is off unless the entity is one whose deleted contents must be recoverable from the trail. */
    CaptureDeleteBeforeImage bool
}

/* Registry is safe for concurrent use: Register is ordinarily a boot-time call, but it is public, returns the registry for chaining and is reachable through Recorder.Registry() for the life of the process, while every recorded write reads the same map from a request goroutine — an unguarded late Register was a fatal concurrent map read and write, not an error any recovery could catch. */
type Registry struct {
    mutex               sync.RWMutex
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
        defaultTable: defaultTable,
        /* copied rather than aliased: the variadic call form shares the caller's backing array, and a caller reusing that slice after boot would mutate what request goroutines read */
        globalIgnoredFields: append([]string{}, globalIgnoredFields...),
        optionsByEntity:     make(map[string]EntityOptions),
    }
}

func (instance *Registry) Register(entity string, options EntityOptions) *Registry {
    if "" != options.Table {
        validateAuditTableName(options.Table)
    }

    instance.mutex.Lock()
    instance.optionsByEntity[entity] = options
    instance.mutex.Unlock()

    return instance
}

func (instance *Registry) EnsureSchema(ctx context.Context, database *bun.DB) error {
    if _, txErr := database.NewCreateTable().Model((*Transaction)(nil)).IfNotExists().Exec(ctx); nil != txErr {
        return exception.NewError("could not create the audit transaction table", map[string]any{"table": DefaultTransactionTable}, txErr)
    }

    for _, table := range instance.distinctTables() {
        if _, createErr := database.NewCreateTable().Model((*Entry)(nil)).ModelTableExpr(table).IfNotExists().Exec(ctx); nil != createErr {
            return exception.NewError("could not create an audit table", map[string]any{"table": table}, createErr)
        }
    }

    return nil
}

func (instance *Registry) tableFor(entity string) string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if options, exists := instance.optionsByEntity[entity]; true == exists && "" != options.Table {
        return options.Table
    }

    return instance.defaultTable
}

func (instance *Registry) capturesDeleteBeforeImageFor(entity string) bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    options, exists := instance.optionsByEntity[entity]
    if false == exists {
        return false
    }

    return options.CaptureDeleteBeforeImage
}

func (instance *Registry) ignoredFieldsFor(entity string) map[string]struct{} {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

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
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    seen := map[string]struct{}{instance.defaultTable: {}}
    tables := []string{instance.defaultTable}
    extra := make([]string, 0, len(instance.optionsByEntity))

    for _, options := range instance.optionsByEntity {
        if "" == options.Table {
            continue
        }
        if _, exists := seen[options.Table]; true == exists {
            continue
        }
        seen[options.Table] = struct{}{}
        extra = append(extra, options.Table)
    }

    /* sorted so EnsureSchema issues its DDL in one order across runs: the map walk is random, and a failure naming "the third table" would otherwise name a different table on every retry */
    sort.Strings(extra)

    return append(tables, extra...)
}
