package internal

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* BootWarningContextKey is the context key every boot warning carries its name under, so an operator or a test can select the boot warnings of a journal by one key */
const BootWarningContextKey = "bootWarning"

/* BootWarning is one hazard a configuration admits and the framework does not refuse, written once into the configured journal. Every boot warning of the framework has this shape: Name is a stable dotted identifier, Message says what the configuration does and what follows from it, and Context names the configuration (the keys "firewall", "entry" and "configurationKey") and never a value that may be a secret. */
type BootWarning struct {
    Name    string
    Message string
    Context loggingcontract.Context
}

/* WriteBootWarning writes the warning at WARNING, its name added under BootWarningContextKey; a nil logger writes nothing */
func WriteBootWarning(logger loggingcontract.Logger, warning BootWarning) {
    if true == IsNilInterface(logger) {
        return
    }

    context := make(loggingcontract.Context, len(warning.Context)+1)
    for key, value := range warning.Context {
        context[key] = value
    }

    context[BootWarningContextKey] = warning.Name

    logger.Warning(warning.Message, context)
}
