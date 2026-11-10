package internal

import (
    "slices"
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
    registry := NewBootWarningRegistry()

    seen := make(map[string]struct{}, len(registry.registrations))
    for _, registration := range registry.registrations {
        if _, duplicated := seen[registration.name]; true == duplicated {
            t.Fatalf("expected each code registered once, %q twice", registration.name)
        }
        seen[registration.name] = struct{}{}

        if false == registry.Known(registration.name) {
            t.Fatalf("expected %q known", registration.name)
        }
    }

    expectedCliNames := []string{BootWarningNameFirewallDoorReachUnknown, BootWarningNameProcessEnvironmentVariableIgnored, BootWarningNameSecretMarkUnmatched}
    if cliNames := registry.CliNames(); false == slices.Equal(expectedCliNames, cliNames) {
        t.Fatalf("expected %v written by a cli process, got %v", expectedCliNames, cliNames)
    }

    for name, expected := range map[string]bool{
        BootWarningNameFirewallDoorReachUnknown:                   true,
        BootWarningNameEnvironmentDefaulted:                       false,
        BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage: false,
        BootWarningNameProcessEnvironmentVariableIgnored:          true,
        BootWarningNameSecretMarkUnmatched:                        true,
        "security.firewallShadowed":                               false,
    } {
        if expected != registry.WrittenInCli(name) {
            t.Fatalf("expected %q written by a cli process %v", name, expected)
        }
    }

    if true == registry.Known("security.firewallShadowed") {
        t.Fatalf("expected a code the framework no longer writes unknown")
    }
}

func TestBootWarningRegistry_DeclareAddsACodeOnceAndKeepsEachRegistryApart(t *testing.T) {
    registry := NewBootWarningRegistry()

    if false == registry.Declare("module.cliHazard", true) || false == registry.Declare("module.httpHazard", false) {
        t.Fatalf("expected two new codes declared")
    }

    if false == registry.Known("module.cliHazard") || false == registry.WrittenInCli("module.cliHazard") {
        t.Fatalf("expected the cli code known and written in cli")
    }

    if false == registry.Known("module.httpHazard") || true == registry.WrittenInCli("module.httpHazard") {
        t.Fatalf("expected the http code known and not written in cli")
    }

    if cliNames := registry.CliNames(); 4 != len(cliNames) || "module.cliHazard" != cliNames[3] {
        t.Fatalf("expected the declared cli code after the framework's, got %v", cliNames)
    }

    for _, name := range []string{"", "module.cliHazard", BootWarningNameEnvironmentDefaulted} {
        if true == registry.Declare(name, true) {
            t.Fatalf("expected %q refused", name)
        }
    }

    if true == registry.WrittenInCli(BootWarningNameEnvironmentDefaulted) {
        t.Fatalf("expected a refused declaration to leave the framework's row as it was")
    }

    if true == NewBootWarningRegistry().Known("module.cliHazard") {
        t.Fatalf("expected a fresh registry to hold none of another's declarations")
    }
}
