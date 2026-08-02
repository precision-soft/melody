package container

import (
    "testing"

    containercontract "github.com/precision-soft/melody/container/contract"
)

/* @info the default decides who closes an installed override, and it is the answer that cannot leak: an override belongs to whoever installed it, because the installer usually still holds it after the scope is gone. A default flipped to closed-with-scope would have the scope close a value its installer goes on using — a use-after-close one request wide, discovered nowhere near the override. */
func TestApplyOverrideOptions_DefaultsToLeavingTheValueWithItsInstaller(t *testing.T) {
    option := applyOverrideOptions(nil)

    if true == option.ClosedWithScope {
        t.Fatalf("expected an installed override to stay its installer's by default")
    }
}

/* @info the one option there is hands the value to the scope's teardown, which is the whole of the mechanism — the teardown already walks the created maps, so nothing about closing has to learn about overrides. A nil option is skipped rather than called. */
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
