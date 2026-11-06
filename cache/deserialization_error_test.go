package cache

import (
    "errors"
    "fmt"
    "testing"
    "time"
)

func TestDeserializationError_IsFoundThroughWrapping(t *testing.T) {
    causeErr := errors.New("unexpected byte")

    deserializationErr := NewDeserializationError([]string{"k"}, causeErr)

    if false == IsDeserializationError(deserializationErr) {
        t.Fatalf("expected the error to identify itself")
    }

    wrappedErr := fmt.Errorf("outer: %w", deserializationErr)
    if false == IsDeserializationError(wrappedErr) {
        t.Fatalf("expected the error to be found through wrapping")
    }

    if false == errors.Is(deserializationErr, causeErr) {
        t.Fatalf("expected the cause to stay reachable through Unwrap")
    }

    if true == IsDeserializationError(causeErr) {
        t.Fatalf("expected a foreign error not to read as a deserialization failure")
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

func TestIsDeserializationError_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == IsDeserializationError(newCyclicChainError()) {
            t.Error("expected a chain without the type to read as another failure")
        }
    })
}
