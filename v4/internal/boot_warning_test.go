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

func (instance *bootWarningRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *bootWarningRecordingLogger) CauseChainDepth() int {
    return 8
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

func TestBootWarningRegistry_NamesEachCodeOnce(t *testing.T) {
    registry := NewBootWarningRegistry()

    expectedNames := []string{
        BootWarningNameFirewallDoorReachUnknown,
        BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage,
        BootWarningNameProcessEnvironmentVariableIgnored,
        BootWarningNameStaticCacheMaxAgeIgnored,
        BootWarningNameRetryPolicyServiceIgnored,
        BootWarningNameServiceBuiltAtBootFailed,
        BootWarningNameCsrfDisabledOnLoginFirewall,
    }
    if names := registry.Names(); false == slices.Equal(expectedNames, names) {
        t.Fatalf("expected %v, got %v", expectedNames, names)
    }

    for _, name := range expectedNames {
        if false == registry.Known(name) {
            t.Fatalf("expected %q known", name)
        }
    }

    registry.Names()[0] = "changed"
    if BootWarningNameFirewallDoorReachUnknown != registry.Names()[0] {
        t.Fatalf("writing into the returned list reached the registry")
    }

    for _, name := range []string{"security.firewallShadowed", "application.environment.defaulted"} {
        if true == registry.Known(name) {
            t.Fatalf("expected %q, a code the framework no longer writes, unknown", name)
        }
    }
}

func TestBootWarningRegistry_DeclareAddsACodeOnceAndKeepsEachRegistryApart(t *testing.T) {
    registry := NewBootWarningRegistry()

    if false == registry.Declare("module.hazard") {
        t.Fatalf("expected a new code declared")
    }

    if false == registry.Known("module.hazard") {
        t.Fatalf("expected the declared code known")
    }

    if names := registry.Names(); 8 != len(names) || "module.hazard" != names[7] {
        t.Fatalf("expected the declared code after the framework's, got %v", names)
    }

    for _, name := range []string{"", "module.hazard", BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage} {
        if true == registry.Declare(name) {
            t.Fatalf("expected %q refused", name)
        }
    }

    if 8 != len(registry.Names()) {
        t.Fatalf("expected a refused declaration to add nothing, got %v", registry.Names())
    }

    if true == NewBootWarningRegistry().Known("module.hazard") {
        t.Fatalf("expected a fresh registry to hold none of another's declarations")
    }
}
