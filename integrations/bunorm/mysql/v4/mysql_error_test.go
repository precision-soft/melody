package mysql

import (
    "errors"
    "fmt"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
)

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
            name:        "mysql error 1062 is a duplicate key",
            inputErr:    &driver.MySQLError{Number: 1062, Message: "Duplicate entry 'a' for key 'PRIMARY'"},
            isDuplicate: true,
        },
        {
            name:        "wrapped mysql error 1062 is a duplicate key",
            inputErr:    fmt.Errorf("insert widget: %w", &driver.MySQLError{Number: 1062, Message: "Duplicate entry"}),
            isDuplicate: true,
        },
        {
            name:        "mysql error with another number is not a duplicate key",
            inputErr:    &driver.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"},
            isDuplicate: false,
        },
        {
            name:        "plain error mentioning 1062 is not a duplicate key",
            inputErr:    errors.New("Error 1062: Duplicate entry"),
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

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func TestIsDuplicateKey_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == IsDuplicateKey(newCyclicChainError()) {
            t.Error("expected a chain without a server error to read as another failure")
        }
    })
}
