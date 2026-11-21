package audit

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

func TestRegistry_RejectsInvalidDefaultTableName(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic for an invalid default table name")
        }

        if "RegistryConfig.DefaultTable" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name RegistryConfig.DefaultTable, got %v", recoveredErr)
        }
    }()

    NewRegistry(RegistryConfig{DefaultTable: "audit; DROP TABLE users"})
}

func TestRegistry_RejectsInvalidEntityTableName(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic for an invalid entity table name")
        }

        if "EntityOptions.Table" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name EntityOptions.Table, got %v", recoveredErr)
        }
    }()

    NewRegistry(RegistryConfig{DefaultTable: "melody_audit"}).Register("order", EntityOptions{Table: "orders`; DROP"})
}

func TestNewRegistry_CopiesTheGlobalIgnoredFields(t *testing.T) {
    fields := []string{"secret"}
    registry := NewRegistry(RegistryConfig{DefaultTable: "melody_audit", GlobalIgnoredFields: fields})

    fields[0] = "email"

    ignored := registry.ignoredFieldsFor("any")
    if _, has := ignored["secret"]; false == has {
        t.Fatalf("expected the registry to keep the field set it was constructed with")
    }
    if _, has := ignored["email"]; true == has {
        t.Fatalf("expected the caller's later mutation of the slice to stay the caller's")
    }
}

func TestRegistry_RegisterConcurrentWithReadsIsSafe(t *testing.T) {
    registry := NewRegistry(RegistryConfig{DefaultTable: "melody_audit"})

    var wait sync.WaitGroup

    for index := 0; index < 8; index++ {
        wait.Add(2)

        entity := fmt.Sprintf("entity_%d", index)

        go func() {
            defer wait.Done()
            registry.Register(entity, EntityOptions{Table: fmt.Sprintf("audit_%s", entity), IgnoredFields: []string{"secret"}})
        }()

        go func() {
            defer wait.Done()
            _ = registry.tableFor(entity)
            _ = registry.capturesDeleteBeforeImageFor(entity)
            _ = registry.ignoredFieldsFor(entity)
            _ = registry.distinctTables()
        }()
    }

    wait.Wait()
}

func auditTestRuntime(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

func TestRegistry_EnsureSchemaRefusesAnAbsentRuntimeBeforeAnyStatement(t *testing.T) {
    registry := NewRegistry(RegistryConfig{DefaultTable: "melody_audit"})

    var typedNilRuntime containercontract.Runtime = (*nilRuntimeForAudit)(nil)
    for _, runtimeInstance := range []containercontract.Runtime{nil, typedNilRuntime, contextlessRuntimeForAudit{}} {
        schemaErr := registry.EnsureSchema(runtimeInstance, newTestDatabase())
        if nil == schemaErr || false == strings.Contains(schemaErr.Error(), "the runtime is nil or carries no context") {
            t.Fatalf("expected the absent runtime refused before any statement, got %v", schemaErr)
        }
    }
}

type nilRuntimeForAudit struct {
    containercontract.Runtime
}

/* a runtime that carries no context, refused like an absent one */
type contextlessRuntimeForAudit struct {
    containercontract.Runtime
}

func (instance contextlessRuntimeForAudit) Context() context.Context {
    return nil
}

func TestRegistry_EnsureSchemaRunsUnderTheRuntimesContext(t *testing.T) {
    registry := NewRegistry(RegistryConfig{DefaultTable: "melody_audit"})
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    schemaErr := registry.EnsureSchema(auditTestRuntime(cancelledContext), newTestDatabase())
    if nil == schemaErr || false == errors.Is(schemaErr, context.Canceled) {
        t.Fatalf("expected the statement run under the runtime's cancelled context, got %v", schemaErr)
    }
}

func TestRegistry_EnsureSchemaNamesTheTableItCouldNotCreate(t *testing.T) {
    registry := NewRegistry(RegistryConfig{DefaultTable: "melody_audit"})

    schemaErr := registry.EnsureSchema(auditTestRuntime(context.Background()), newTestDatabase())
    if nil == schemaErr {
        t.Fatalf("expected the offline database to fail the schema creation")
    }

    if false == strings.Contains(schemaErr.Error(), "could not create the audit transaction table") {
        t.Fatalf("expected the failure to say which step broke, got: %v", schemaErr)
    }

    logContext := exception.LogContext(schemaErr, exception.LogContextConfig{})
    if DefaultTransactionTable != logContext["table"] {
        t.Fatalf("expected the failure to name the table, got context: %v", logContext)
    }
}

/* Register copies the ignored fields, so a caller appending to its own slice afterwards does not change what the registry reads */
func TestRegistry_RegisterCopiesTheIgnoredFields(t *testing.T) {
    ignoredFields := make([]string, 1, 4)
    ignoredFields[0] = "password"

    registry := NewRegistry(RegistryConfig{DefaultTable: "audit_log"})
    registry.Register("account", EntityOptions{IgnoredFields: ignoredFields})

    ignoredFields[0] = "email"
    _ = append(ignoredFields, "token")

    ignored := registry.ignoredFieldsFor("account")
    if _, isIgnored := ignored["password"]; false == isIgnored {
        t.Fatalf("expected the registered field kept, got %v", ignored)
    }

    if _, isIgnored := ignored["email"]; true == isIgnored {
        t.Fatalf("expected the caller's later write not to reach the registry, got %v", ignored)
    }
}
