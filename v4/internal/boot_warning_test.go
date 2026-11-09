package internal

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type bootWarningRecordingLogger struct {
    messages []string
    contexts []loggingcontract.Context
}

func (instance *bootWarningRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *bootWarningRecordingLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *bootWarningRecordingLogger) Info(message string, context loggingcontract.Context) {}

func (instance *bootWarningRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
    instance.contexts = append(instance.contexts, context)
}

func (instance *bootWarningRecordingLogger) Error(message string, context loggingcontract.Context) {}

func (instance *bootWarningRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func TestWriteBootWarning_WritesAtWarningWithItsNameAndLeavesTheContextItWasGiven(t *testing.T) {
    logger := &bootWarningRecordingLogger{}
    warningContext := loggingcontract.Context{"firewall": "api"}

    WriteBootWarning(logger, BootWarning{Name: BootWarningNameFirewallDoorReachUnknown, Message: "shadowed", Context: warningContext})

    if 1 != len(logger.messages) || "shadowed" != logger.messages[0] {
        t.Fatalf("expected one warning record, got %v", logger.messages)
    }

    if BootWarningNameFirewallDoorReachUnknown != logger.contexts[0][BootWarningContextKey] || "api" != logger.contexts[0]["firewall"] {
        t.Fatalf("expected the name under %q beside the given context, got %v", BootWarningContextKey, logger.contexts[0])
    }

    if _, written := warningContext[BootWarningContextKey]; true == written {
        t.Fatalf("expected the warning's own context to be left as it was given")
    }

    WriteBootWarning(nil, BootWarning{Name: "x", Message: "x"})
}

func TestBootWarningRegistry_NamesEachCodeOnceAndMarksWhatACliProcessWrites(t *testing.T) {
    seen := make(map[string]struct{}, len(bootWarningRegistry))
    for _, registration := range bootWarningRegistry {
        if _, duplicated := seen[registration.name]; true == duplicated {
            t.Fatalf("expected each code registered once, %q twice", registration.name)
        }
        seen[registration.name] = struct{}{}

        if false == IsKnownBootWarningName(registration.name) {
            t.Fatalf("expected %q known", registration.name)
        }
    }

    if cliNames := CliBootWarningNames(); 1 != len(cliNames) || BootWarningNameFirewallDoorReachUnknown != cliNames[0] {
        t.Fatalf("expected the door reach code alone written by a cli process, got %v", cliNames)
    }

    for name, expected := range map[string]bool{
        BootWarningNameFirewallDoorReachUnknown:                   true,
        BootWarningNameEnvironmentDefaulted:                       false,
        BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage: false,
        "security.firewallShadowed":                               false,
    } {
        if expected != IsCliBootWarningName(name) {
            t.Fatalf("expected %q written by a cli process %v", name, expected)
        }
    }

    if true == IsKnownBootWarningName("security.firewallShadowed") {
        t.Fatalf("expected a code the framework no longer writes unknown")
    }
}
