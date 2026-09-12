package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func TestApplyOverrideOptions_DefaultsToLeavingTheValueWithItsInstaller(t *testing.T) {
    option := applyOverrideOptions(nil)

    if true == option.ClosedWithScope {
        t.Fatalf("expected an installed override to stay its installer's by default")
    }
}

func TestOverrideOptions_ClosedWithScopeHandsTheValueToTheTeardown(t *testing.T) {
    option := applyOverrideOptions([]containercontract.OverrideOption{ClosedWithScope()})

    if false == option.ClosedWithScope {
        t.Fatalf("expected the value to be handed to the scope's teardown")
    }

    withNil := applyOverrideOptions([]containercontract.OverrideOption{nil, ClosedWithScope(), nil})

    if false == withNil.ClosedWithScope {
        t.Fatalf("expected a nil option to be skipped rather than to take the call down")
    }
}
