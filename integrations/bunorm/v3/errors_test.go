package bunorm

import (
    "errors"
    "net"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

/* the link answers the class through Is and hands the driver failure on through Unwrap, and a journal rendering the exception it sits under reads the class once, where the driver failure stood */
func TestDatabaseUnreachable_AnswersTheClassAndKeepsTheDriverFailureReachable(t *testing.T) {
    driverErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
    openErr := exception.NewError("database connection failed", map[string]any{"host": "replica.internal"}, DatabaseUnreachable(driverErr))

    if false == errors.Is(openErr, ErrDatabaseUnreachable) {
        t.Fatalf("expected the class to answer through the exception, got %v", openErr)
    }

    var reached *net.OpError
    if false == errors.As(openErr, &reached) || driverErr != reached {
        t.Fatalf("expected the driver failure reachable through the link, got %v", openErr)
    }

    /* the driver failure unwraps further on its own (an OpError to its inner error), so the chain is read on its head and on the count of the class, not on its length */
    chain, isChain := exception.LogContext(openErr)["causeChain"].([]string)
    if false == isChain || 2 > len(chain) || ErrDatabaseUnreachable.Error() != chain[0] || driverErr.Error() != chain[1] {
        t.Fatalf("expected the chain to read the class first and the driver failure right under it, got %v", exception.LogContext(openErr)["causeChain"])
    }

    classCount := 0
    for _, link := range chain {
        if ErrDatabaseUnreachable.Error() == link {
            classCount++
        }
    }
    if 1 != classCount {
        t.Fatalf("expected the class rendered exactly once in the chain, got %v", chain)
    }

    if true == errors.Is(exception.NewError("database connection failed", nil, driverErr), ErrDatabaseUnreachable) {
        t.Fatal("a failure nobody filed under the class must not answer it")
    }
}
