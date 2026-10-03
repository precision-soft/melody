package internal

import (
    "testing"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

    WriteBootWarning(logger, BootWarning{Name: "security.firewallShadowed", Message: "shadowed", Context: warningContext})

    if 1 != len(logger.messages) || "shadowed" != logger.messages[0] {
        t.Fatalf("expected one warning record, got %v", logger.messages)
    }

    if "security.firewallShadowed" != logger.contexts[0][BootWarningContextKey] || "api" != logger.contexts[0]["firewall"] {
        t.Fatalf("expected the name under %q beside the given context, got %v", BootWarningContextKey, logger.contexts[0])
    }

    if _, written := warningContext[BootWarningContextKey]; true == written {
        t.Fatalf("expected the warning's own context to be left as it was given")
    }

    WriteBootWarning(nil, BootWarning{Name: "x", Message: "x"})
}
