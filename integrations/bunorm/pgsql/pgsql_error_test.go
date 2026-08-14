package pgsql

import (
    "errors"
    "fmt"
    "testing"

    "github.com/precision-soft/melody/exception"
)

/* fakeSqlStateError plays the pgdriver error shape: the driver exports no constructor for its type, so the typed path is proved through the same interface production matches. */
type fakeSqlStateError struct {
    sqlState string
}

func (instance *fakeSqlStateError) Error() string {
    return fmt.Sprintf("ERROR: fake (SQLSTATE=%s)", instance.sqlState)
}

func (instance *fakeSqlStateError) Field(field byte) string {
    if 'C' == field {
        return instance.sqlState
    }

    return ""
}

func TestIsDuplicateKey(t *testing.T) {
    testCases := []struct {
        name        string
        inputErr    error
        isDuplicate bool
    }{
        {
            name:        "nil error is not a duplicate key",
            inputErr:    nil,
            isDuplicate: false,
        },
        {
            name:        "typed sqlstate 23505 is a duplicate key",
            inputErr:    &fakeSqlStateError{sqlState: "23505"},
            isDuplicate: true,
        },
        {
            name:        "typed sqlstate 23505 wrapped in a plain error is a duplicate key",
            inputErr:    fmt.Errorf("insert widget: %w", &fakeSqlStateError{sqlState: "23505"}),
            isDuplicate: true,
        },
        {
            /* the exception wrapper's message never renders its cause, so any probe of the rendered text alone would answer false here; the typed match must see through the wrapping */
            name:        "typed sqlstate 23505 wrapped in an exception is a duplicate key",
            inputErr:    exception.NewError("insert widget failed", nil, &fakeSqlStateError{sqlState: "23505"}),
            isDuplicate: true,
        },
        {
            name:        "typed syntax error is not a duplicate key",
            inputErr:    &fakeSqlStateError{sqlState: "42601"},
            isDuplicate: false,
        },
        {
            /* the verdict is pinned to the typed SQLSTATE: a message that merely contains the digits — a quoted value, a constraint name — carries no protocol error and must not be read as one */
            name:        "untyped message containing the digits is not a duplicate key",
            inputErr:    errors.New("ERROR: value (1235051) violates check constraint \"widget_check\""),
            isDuplicate: false,
        },
        {
            name:        "untyped duplicate key message is not a duplicate key",
            inputErr:    errors.New("pq: duplicate key value violates unique constraint"),
            isDuplicate: false,
        },
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            if testCase.isDuplicate != IsDuplicateKey(testCase.inputErr) {
                t.Fatalf("expected IsDuplicateKey=%v for %v", testCase.isDuplicate, testCase.inputErr)
            }
        })
    }
}
