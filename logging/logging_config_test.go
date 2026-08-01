package logging

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* @info the labels end up read lock-free on every Log call of the logger built from this configuration: a live reference kept by the registering module — or handed out by the accessor — turns a later write into a fatal concurrent map access, so both directions copy */
func TestNewLoggingConfiguration_CopiesTheLabelsBothWays(t *testing.T) {
    labels := loggingcontract.LevelLabels{
        loggingcontract.LevelError: loggingcontract.LevelLabelFromString("configured-error"),
    }

    configuration := NewLoggingConfiguration(labels)

    labels[loggingcontract.LevelError] = loggingcontract.LevelLabelFromString("mutated-after-construction")

    heldLabels := configuration.LevelLabels()
    if "configured-error" != heldLabels[loggingcontract.LevelError].String() {
        t.Fatalf("expected the configuration to keep the labels it was built with, got %s", heldLabels[loggingcontract.LevelError].String())
    }

    heldLabels[loggingcontract.LevelError] = loggingcontract.LevelLabelFromString("mutated-through-accessor")

    reReadLabels := configuration.LevelLabels()
    if "configured-error" != reReadLabels[loggingcontract.LevelError].String() {
        t.Fatalf("expected the accessor to hand out a copy, got %s", reReadLabels[loggingcontract.LevelError].String())
    }
}
