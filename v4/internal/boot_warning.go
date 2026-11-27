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
    BootWarningNameStaticCacheMaxAgeIgnored                   = "http.static.cacheMaxAgeIgnored"
    BootWarningNameRetryPolicyServiceIgnored                  = "messagebus.retryPolicyServiceIgnored"
    BootWarningNameServiceBuiltAtBootFailed                   = "application.service.builtAtBootFailed"
    BootWarningNameCsrfDisabledOnLoginFirewall                = "security.csrfDisabledOnLoginFirewall"
)

/* BootWarningRegistry holds the codes one application writes: the framework's own rows, then the ones its modules declare at boot, so two applications in one process never see each other's declarations */
type BootWarningRegistry struct {
    names []string
}

/* NewBootWarningRegistry answers a registry holding the framework's own codes */
func NewBootWarningRegistry() *BootWarningRegistry {
    return &BootWarningRegistry{
        names: []string{
            BootWarningNameFirewallDoorReachUnknown,
            BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage,
            BootWarningNameProcessEnvironmentVariableIgnored,
            BootWarningNameStaticCacheMaxAgeIgnored,
            BootWarningNameRetryPolicyServiceIgnored,
            BootWarningNameServiceBuiltAtBootFailed,
            BootWarningNameCsrfDisabledOnLoginFirewall,
        },
    }
}

/* Declare adds a code; false when the name is empty or already held, since a code has one owner */
func (instance *BootWarningRegistry) Declare(name string) bool {
    if "" == name || true == instance.Known(name) {
        return false
    }

    instance.names = append(instance.names, name)

    return true
}

/* Names answers the name of every code, in registration order */
func (instance *BootWarningRegistry) Names() []string {
    return append([]string{}, instance.names...)
}

func (instance *BootWarningRegistry) Known(name string) bool {
    for _, registeredName := range instance.names {
        if name == registeredName {
            return true
        }
    }

    return false
}

/* BootWarning is one hazard a configuration admits and the framework does not refuse, written once into the configured journal. Every boot warning of the framework has this shape: Name is a stable dotted identifier, Message says what the configuration does and what follows from it, and Context names the configuration (the keys "firewall", "entry", "configurationKey" and "serviceName"), and never a value that may be a secret; "cause" carries a failure's own text, which its author keeps free of secrets. */
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
