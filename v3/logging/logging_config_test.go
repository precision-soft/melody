package logging

import (
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

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

/* nilLevelLabelsConfiguration carries its method on the pointer, so a nil pointer of this type satisfies the configuration interface while every call through it dereferences the nil receiver — the shape a module registers when it declares its configuration and never assigns it. */
type nilLevelLabelsConfiguration struct{}

func (instance *nilLevelLabelsConfiguration) LevelLabels() loggingcontract.LevelLabels {
    return instance.mustNotBeReached()
}

func (instance *nilLevelLabelsConfiguration) mustNotBeReached() loggingcontract.LevelLabels {
    panic("the nil receiver was dereferenced")
}

var _ loggingcontract.LoggingConfiguration = (*nilLevelLabelsConfiguration)(nil)

func TestLoggingConfigurationFromModules_TypedNilIsRefusedByName(t *testing.T) {
    var typedNil *nilLevelLabelsConfiguration

    moduleConfigurations := map[string]any{
        loggingcontract.LoggingConfigurationName: typedNil,
    }

    assertPanicsWithEmergencyNamingTheConfiguration(t, func() {
        _ = LoggingConfigurationFromModules(moduleConfigurations)
    })
}

func assertPanicsWithEmergencyNamingTheConfiguration(t *testing.T, callback func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed nil to be refused")
        }

        err, isError := recovered.(*exception.Error)
        if false == isError {
            t.Fatalf("expected a melody emergency, got %T: %v", recovered, recovered)
        }

        if "invalid logging configuration" != err.Message() {
            t.Fatalf("unexpected refusal message: %q", err.Message())
        }

        actualType := err.Context()["actualType"]
        if "*logging.nilLevelLabelsConfiguration" != actualType {
            t.Fatalf("expected the refusal to name the registered type, got %v", actualType)
        }
    }()

    callback()
}
