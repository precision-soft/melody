package cache

import (
    "errors"
    "fmt"
    "testing"
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
