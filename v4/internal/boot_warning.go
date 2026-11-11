package internal

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* BootWarningContextKey is the context key every boot warning carries its name under, so an operator or a test can select the boot warnings of a journal by one key */
const BootWarningContextKey = "bootWarning"

const (
    BootWarningNameFirewallDoorReachUnknown                   = "security.firewallDoorReachUnknown"
    BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage = "http.session.unboundedTtlOnBoundedDefaultStorage"
    BootWarningNameProcessEnvironmentVariableIgnored          = "application.environment.processVariableIgnored"
    BootWarningNameSecretMarkUnmatched                        = "application.secretMark.unmatched"
)

type bootWarningRegistration struct {
    name         string
    writtenInCli bool
}

/* BootWarningRegistry holds the codes one application writes: the framework's own rows, then the ones its modules declare at boot, so two applications in one process never see each other's declarations */
type BootWarningRegistry struct {
    registrations []bootWarningRegistration
}

/* NewBootWarningRegistry answers a registry holding the framework's own codes */
func NewBootWarningRegistry() *BootWarningRegistry {
    return &BootWarningRegistry{
        registrations: []bootWarningRegistration{
            {name: BootWarningNameFirewallDoorReachUnknown, writtenInCli: true},
            {name: BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage, writtenInCli: false},
            {name: BootWarningNameProcessEnvironmentVariableIgnored, writtenInCli: true},
            {name: BootWarningNameSecretMarkUnmatched, writtenInCli: true},
        },
    }
}

/* Declare adds a code; false when the name is empty or already held, since a code has one owner */
func (instance *BootWarningRegistry) Declare(name string, writtenInCli bool) bool {
    if "" == name || true == instance.Known(name) {
        return false
    }

    instance.registrations = append(instance.registrations, bootWarningRegistration{name: name, writtenInCli: writtenInCli})

    return true
}

/* CliNames answers the name of every code a cli process can write, in registration order */
func (instance *BootWarningRegistry) CliNames() []string {
    names := make([]string, 0, len(instance.registrations))
    for _, registration := range instance.registrations {
        if true == registration.writtenInCli {
            names = append(names, registration.name)
        }
    }

    return names
}

func (instance *BootWarningRegistry) Known(name string) bool {
    for _, registration := range instance.registrations {
        if name == registration.name {
            return true
        }
    }

    return false
}

func (instance *BootWarningRegistry) WrittenInCli(name string) bool {
    for _, registration := range instance.registrations {
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
