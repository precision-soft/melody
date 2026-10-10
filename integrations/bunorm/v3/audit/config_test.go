package audit

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestRegistry_RejectsInvalidDefaultTableName(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for an invalid default table name")
        }
    }()

    NewRegistry("audit; DROP TABLE users")
}

func TestRegistry_RejectsInvalidEntityTableName(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for an invalid entity table name")
        }
    }()

    NewRegistry("melody_audit").Register("order", EntityOptions{Table: "orders`; DROP"})
}

func TestNewRegistry_CopiesTheGlobalIgnoredFields(t *testing.T) {
    fields := []string{"secret"}
    registry := NewRegistry("melody_audit", fields...)

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
    registry := NewRegistry("melody_audit")

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

func TestRegistry_EnsureSchemaNamesTheTableItCouldNotCreate(t *testing.T) {
    registry := NewRegistry("melody_audit")

    schemaErr := registry.EnsureSchema(context.Background(), newTestDatabase())
    if nil == schemaErr {
        t.Fatalf("expected the offline database to fail the schema creation")
    }

    if false == strings.Contains(schemaErr.Error(), "could not create the audit transaction table") {
        t.Fatalf("expected the failure to say which step broke, got: %v", schemaErr)
    }

    logContext := exception.LogContext(schemaErr)
    if DefaultTransactionTable != logContext["table"] {
        t.Fatalf("expected the failure to name the table, got context: %v", logContext)
    }
}

/* Register copies the ignored fields, so a caller appending to its own slice afterwards does not change what the registry reads */
func TestRegistry_RegisterCopiesTheIgnoredFields(t *testing.T) {
    ignoredFields := make([]string, 1, 4)
    ignoredFields[0] = "password"

    registry := NewRegistry("audit_log")
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
