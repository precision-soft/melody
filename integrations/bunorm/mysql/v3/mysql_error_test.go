package mysql

import (
    "errors"
    "fmt"
    "testing"

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
