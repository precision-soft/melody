package logging

import (
    "testing"

    "github.com/precision-soft/melody/exception"
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

/* @info this is the one reader of the module-configuration registry, and it decides the labels of every record the process writes. Absent configuration is the ordinary case and must land on the defaults rather than on an empty label map — an empty one renders every level as its own name only by accident of the fallback */
func TestLoggingConfigurationFromModules_WithoutConfiguration_UsesTheDefaults(t *testing.T) {
    for _, moduleConfigurations := range []map[string]any{nil, {}, {"other": "value"}} {
        configuration := LoggingConfigurationFromModules(moduleConfigurations)

        if nil == configuration {
            t.Fatalf("expected a configuration for %v", moduleConfigurations)
        }

        labels := configuration.LevelLabels()

        if len(loggingcontract.DefaultLevelLabels()) != len(labels) {
            t.Fatalf("expected the default labels for %v, got %v", moduleConfigurations, labels)
        }

        if "emergency" != labels.LabelFor(loggingcontract.LevelEmergency).String() {
            t.Fatalf("expected the default emergency label, got %q", labels.LabelFor(loggingcontract.LevelEmergency).String())
        }
    }
}

func TestLoggingConfigurationFromModules_WithConfiguration_ReturnsIt(t *testing.T) {
    registered := NewLoggingConfiguration(loggingcontract.LevelLabels{
        loggingcontract.LevelError: loggingcontract.LevelLabelFromInt(3),
    })

    resolved := LoggingConfigurationFromModules(map[string]any{
        loggingcontract.LoggingConfigurationName: registered,
    })

    if registered != resolved {
        t.Fatalf("expected the registered configuration to be returned")
    }

    if "3" != resolved.LevelLabels().LabelFor(loggingcontract.LevelError).String() {
        t.Fatalf("unexpected label: %q", resolved.LevelLabels().LabelFor(loggingcontract.LevelError).String())
    }
}

/* assertPanicsWithReportedType pins which of the two refusals fired. Both carry the same message, so a message-only assertion cannot tell them apart — and they shadow each other: with the nil guard gone, a nil raw value fails the type assertion and the second guard panics with the identical message, which would leave a deleted guard indistinguishable from a working one. The reported type is the observable that separates them. */
func assertPanicsWithReportedType(t *testing.T, callback func(), expectedActualType string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected a panic naming the actual type %q", expectedActualType)
        }

        recoveredError, isError := recoveredValue.(*exception.Error)
        if false == isError || nil == recoveredError {
            t.Fatalf("expected the panic to carry an *exception.Error, got %#v", recoveredValue)
        }

        if "invalid logging configuration" != recoveredError.Message() {
            t.Fatalf("unexpected message %q", recoveredError.Message())
        }

        if expectedActualType != recoveredError.Context()["actualType"] {
            t.Fatalf(
                "expected the refusal to name the actual type %q, got %v",
                expectedActualType,
                recoveredError.Context()["actualType"],
            )
        }
    }()

    callback()
}

/* @info a key present under the logging name but carrying nothing is a wiring mistake, not an absence: falling back to the defaults would hide it forever, and the value is read once at boot where the panic names it */
func TestLoggingConfigurationFromModules_NilConfiguration_Panics(t *testing.T) {
    assertPanicsWithReportedType(
        t,
        func() {
            _ = LoggingConfigurationFromModules(map[string]any{
                loggingcontract.LoggingConfigurationName: nil,
            })
        },
        "<nil>",
    )
}

/* @info a value of the wrong type under the logging name is the same wiring mistake wearing another face; the refusal names the type it got so the mistake is readable without a debugger */
func TestLoggingConfigurationFromModules_WrongType_Panics(t *testing.T) {
    assertPanicsWithReportedType(
        t,
        func() {
            _ = LoggingConfigurationFromModules(map[string]any{
                loggingcontract.LoggingConfigurationName: "not a logging configuration",
            })
        },
        "string",
    )
}
