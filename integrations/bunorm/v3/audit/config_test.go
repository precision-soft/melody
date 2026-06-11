package audit

import (
    "testing"
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
