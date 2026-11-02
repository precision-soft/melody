package security

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

const (
    ServiceFirewallManager = "service.security.firewall_manager"
)

func FirewallManagerMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) securitycontract.FirewallManager {
    return container.MustFromResolver[securitycontract.FirewallManager](ctx, serviceContainer, ServiceFirewallManager)
}

func FirewallManagerFromContainer(ctx context.Context, serviceContainer containercontract.Container) securitycontract.FirewallManager {
    firewallManagerInstance, err := container.FromResolver[securitycontract.FirewallManager](ctx, serviceContainer, ServiceFirewallManager)
    if nil == firewallManagerInstance || nil != err {
        return nil
    }

    return firewallManagerInstance
}

func FirewallManagerMustFromResolver(ctx context.Context, resolver containercontract.Resolver) securitycontract.FirewallManager {
    return container.MustFromResolver[securitycontract.FirewallManager](ctx, resolver, ServiceFirewallManager)
}

func FirewallManagerFromResolver(ctx context.Context, resolver containercontract.Resolver) securitycontract.FirewallManager {
    firewallManagerInstance, err := container.FromResolver[securitycontract.FirewallManager](ctx, resolver, ServiceFirewallManager)
    if nil == firewallManagerInstance || nil != err {
        return nil
    }

    return firewallManagerInstance
}

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

    scope.MustOverrideProtectedInstance(securitycontract.ServiceSecurityContext, securityContext, container.OverrideOptions{})
}

func SecurityContextFromRuntime(runtimeInstance runtimecontract.Runtime) (*SecurityContext, bool) {
    if true == internal.IsNilInterface(runtimeInstance) {
        return nil, false
    }

    /* absence answers not-found rather than panicking: IsGranted is reached from goroutines no recover covers */
    scope := runtimeInstance.Scope()
    if true == internal.IsNilInterface(scope) {
        return nil, false
    }

    exists := scope.Has(securitycontract.ServiceSecurityContext)
    if false == exists {
        return nil, false
    }

    securityContext, err := container.FromResolver[*SecurityContext](runtimeInstance.Context(), scope, securitycontract.ServiceSecurityContext)

    if nil != err {
        /* the logger is resolved without panicking: a handler may call IsGranted from a goroutine that outlives the request, after the kernel closed the scope */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Error(
                "failed to resolve security context",
                exception.LogContext(err, exception.LogContextConfig{}),
            )
        }

        return nil, false
    }

    return securityContext, true
}
