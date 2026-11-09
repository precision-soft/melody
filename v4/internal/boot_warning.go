package internal

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* BootWarningContextKey is the context key every boot warning carries its name under, so an operator or a test can select the boot warnings of a journal by one key */
const BootWarningContextKey = "bootWarning"

const (
    BootWarningNameFirewallDoorReachUnknown                   = "security.firewallDoorReachUnknown"
    BootWarningNameEnvironmentDefaulted                       = "application.environment.defaulted"
    BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage = "http.session.unboundedTtlOnBoundedDefaultStorage"
)

type bootWarningRegistration struct {
    name         string
    writtenInCli bool
}

/* a warning a module writes at the first use of its service is not collected, so it is not registered */
var bootWarningRegistry = []bootWarningRegistration{
    {name: BootWarningNameFirewallDoorReachUnknown, writtenInCli: true},
    {name: BootWarningNameEnvironmentDefaulted, writtenInCli: false},
    {name: BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, writtenInCli: false},
}

/* CliBootWarningNames answers the name of every boot warning a cli process can write, in registry order */
func CliBootWarningNames() []string {
    names := make([]string, 0, len(bootWarningRegistry))
    for _, registration := range bootWarningRegistry {
        if true == registration.writtenInCli {
            names = append(names, registration.name)
        }
    }

    return names
}

/* IsKnownBootWarningName reports whether the framework emits a boot warning of that name */
func IsKnownBootWarningName(name string) bool {
    for _, registration := range bootWarningRegistry {
        if name == registration.name {
            return true
        }
    }

    return false
}

/* IsCliBootWarningName reports whether a cli process can write a boot warning of that name */
func IsCliBootWarningName(name string) bool {
    for _, registration := range bootWarningRegistry {
        if name == registration.name {
            return registration.writtenInCli
        }
    }

    return false
}

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
