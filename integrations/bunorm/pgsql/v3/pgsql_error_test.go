package pgsql

import (
    "errors"
    "fmt"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
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

/* fakeSqlStateReporter plays the shape pgx and lib/pq give the same protocol error: the SQLSTATE through SQLState(), and no Field */
type fakeSqlStateReporter struct {
    sqlState string
}

func (instance *fakeSqlStateReporter) Error() string {
    return fmt.Sprintf("ERROR: fake (SQLSTATE %s)", instance.sqlState)
}

func (instance *fakeSqlStateReporter) SQLState() string {
    return instance.sqlState
}

/* a consumer running bun over pgx or lib/pq reaches this door with a typed error that carries its SQLSTATE through SQLState() rather than Field: read through that shape too, a collision on such a driver answers true, and the message stays no identity */
func TestIsDuplicateKey_ReadsTheSqlStateTheOtherDriversReport(t *testing.T) {
    if false == IsDuplicateKey(&fakeSqlStateReporter{sqlState: "23505"}) {
        t.Fatal("expected a SQLState() 23505 to be a duplicate key")
    }

    if false == IsDuplicateKey(exception.NewError("insert widget failed", nil, &fakeSqlStateReporter{sqlState: "23505"})) {
        t.Fatal("expected the wrapped SQLState() 23505 to be a duplicate key")
    }

    if true == IsDuplicateKey(&fakeSqlStateReporter{sqlState: "42601"}) {
        t.Fatal("expected a SQLState() syntax error not to be a duplicate key")
    }
}
