package security

import (
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
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

func SecurityContextSetOnRuntime(runtimeInstance runtimecontract.Runtime, securityContext *SecurityContext) {
    if nil == runtimeInstance {
        exception.Panic(exception.NewError("runtime is nil for security context", nil, nil))
    }

    if nil == securityContext {
        exception.Panic(exception.NewError("security context is nil for runtime", nil, nil))
    }

    runtimeInstance.Scope().MustOverrideProtectedInstance(securitycontract.ServiceSecurityContext, securityContext)
}

func SecurityContextFromRuntime(runtimeInstance runtimecontract.Runtime) (*SecurityContext, bool) {
    if nil == runtimeInstance {
        return nil, false
    }

    exists := runtimeInstance.Scope().Has(securitycontract.ServiceSecurityContext)
    if false == exists {
        return nil, false
    }

    securityContext, err := container.FromResolver[*SecurityContext](runtimeInstance.Scope(), securitycontract.ServiceSecurityContext)

    if nil != err {
        logging.LoggerMustFromRuntime(runtimeInstance).Error(
            "failed to resolve security context",
            exception.LogContext(err),
        )

        return nil, false
    }

    return securityContext, true
}
