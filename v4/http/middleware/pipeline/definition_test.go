package pipeline

import (
    "testing"
)

func TestNewInactiveMiddleware_CarriesBothHalvesOfTheExplanation(t *testing.T) {
    inactive := NewInactiveMiddleware("compression", "not enabled in this environment")

    if "compression" != inactive.Name() {
        t.Fatalf("unexpected name: %q", inactive.Name())
    }

    if "not enabled in this environment" != inactive.Reason() {
        t.Fatalf("unexpected reason: %q", inactive.Reason())
    }
}

func TestCopyDefinitionList_KeepsNilAndCopiesAList(t *testing.T) {
    if nil != copyDefinitionList(nil) {
        t.Fatalf("expected nil to stay nil")
    }

    values := []string{"cors"}
    copied := copyDefinitionList(values)
    values[0] = "session"

    if "cors" != copied[0] {
        t.Fatalf("expected a copy, got %q", copied[0])
    }
}
