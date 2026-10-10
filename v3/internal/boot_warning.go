package internal

import (
    "sync"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

/* BootWarningsOnce holds the boot warnings of an object an application constructs itself, outside the framework's boot, and writes them at the object's first use, once: the first use is the first moment a configured journal is in reach of the object. A use without a logger in reach writes nothing and leaves the warnings for the next one. */
type BootWarningsOnce struct {
    mutex    sync.Mutex
    written  bool
    warnings []BootWarning
}

func NewBootWarningsOnce(warnings []BootWarning) *BootWarningsOnce {
    return &BootWarningsOnce{
        warnings: append([]BootWarning{}, warnings...),
    }
}

/* Warnings answers the warnings held, written or not */
func (instance *BootWarningsOnce) Warnings() []BootWarning {
    if nil == instance {
        return nil
    }

    return append([]BootWarning{}, instance.warnings...)
}

/* Write writes the held warnings through logger the first time it is called with one */
func (instance *BootWarningsOnce) Write(logger loggingcontract.Logger) {
    if nil == instance || 0 == len(instance.warnings) || true == IsNilInterface(logger) {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.written {
        return
    }
    instance.written = true

    for _, warning := range instance.warnings {
        WriteBootWarning(logger, warning)
    }
}
