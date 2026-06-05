package amqp

import (
    "testing"
)

func TestMessageTypeName_NilDoesNotPanic(t *testing.T) {
    if "<nil>" != messageTypeName(nil) {
        t.Fatalf("expected a placeholder name for a nil message, got %q", messageTypeName(nil))
    }
}

func TestMessageTypeName_ReportsConcreteType(t *testing.T) {
    type sample struct{}

    if "amqp.sample" != messageTypeName(sample{}) {
        t.Fatalf("unexpected type name: %q", messageTypeName(sample{}))
    }
}
