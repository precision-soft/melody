package security

import (
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
)

const (
    ServiceFirewallManager = "service.security.firewall_manager"
)

func FirewallManagerMustFromContainer(serviceContainer containercontract.Container) securitycontract.FirewallManager {
    return container.MustFromResolver[securitycontract.FirewallManager](serviceContainer, ServiceFirewallManager)
}

func FirewallManagerFromContainer(serviceContainer containercontract.Container) securitycontract.FirewallManager {
    firewallManagerInstance, err := container.FromResolver[securitycontract.FirewallManager](serviceContainer, ServiceFirewallManager)
    if nil == firewallManagerInstance || nil != err {
        return nil
    }

    return firewallManagerInstance
}

/* the runtime and its scope are read through the interface, the way every other door that takes a substitutable runtime reads them: Runtime is an interface application code may implement, so a typed nil passes a plain comparison and the method call below reaches a nil receiver. runtime.New refuses such a scope at construction, which leaves these two doors as the entries where one can still arrive. */
func SecurityContextSetOnRuntime(runtimeInstance runtimecontract.Runtime, securityContext *SecurityContext) {
    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(exception.NewError("runtime is nil for security context", nil, nil))
    }

    if nil == securityContext {
        exception.Panic(exception.NewError("security context is nil for runtime", nil, nil))
    }

    scope := runtimeInstance.Scope()
    if true == internal.IsNilInterface(scope) {
        exception.Panic(exception.NewError("runtime scope is nil for security context", nil, nil))
    }

    scope.MustOverrideProtectedInstance(securitycontract.ServiceSecurityContext, securityContext)
}

func SecurityContextFromRuntime(runtimeInstance runtimecontract.Runtime) (*SecurityContext, bool) {
    if true == internal.IsNilInterface(runtimeInstance) {
        return nil, false
    }

    /* absence answers not-found rather than panicking, for the same reason the resolution failure below does: IsGranted reaches here from goroutines no recover covers, so the refusal has to be a denial the caller can act on */
    scope := runtimeInstance.Scope()
    if true == internal.IsNilInterface(scope) {
        return nil, false
    }

    exists := scope.Has(securitycontract.ServiceSecurityContext)
    if false == exists {
        return nil, false
    }

    securityContext, err := container.FromResolver[*SecurityContext](scope, securitycontract.ServiceSecurityContext)

    if nil != err {
        /* resolve the logger without panicking: this runs from IsGranted, which a handler can call from a goroutine that outlives the request, and the kernel closes the scope on the way out; LoggerMustFromRuntime would turn a closed-scope read into a fatal panic in a goroutine no recover covers */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "failed to resolve security context",
                exception.LogContext(err),
            )
        }

        return nil, false
    }

    return securityContext, true
}
