package pgsql

import (
    "errors"
    "fmt"
    "testing"
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
            name:        "sqlstate 23505 is a duplicate key",
            inputErr:    errors.New("ERROR: duplicate key value violates unique constraint \"widget_pkey\" (SQLSTATE=23505)"),
            isDuplicate: true,
        },
        {
            name:        "duplicate key message without sqlstate is a duplicate key",
            inputErr:    errors.New("pq: duplicate key value violates unique constraint"),
            isDuplicate: true,
        },
        {
            name:        "wrapped duplicate key error is a duplicate key",
            inputErr:    fmt.Errorf("insert widget: %w", errors.New("SQLSTATE=23505")),
            isDuplicate: true,
        },
        {
            name:        "syntax error is not a duplicate key",
            inputErr:    errors.New("ERROR: syntax error at or near \"select\" (SQLSTATE=42601)"),
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
