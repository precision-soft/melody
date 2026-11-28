package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type erroringResolverScope struct {
    *testScope
}

func (instance *erroringResolverScope) Has(serviceName string) bool {
    return true
}

func (instance *erroringResolverScope) Get(ctx context.Context, serviceName string) (any, error) {
    return nil, errors.New("scope is closed")
}

type substitutableRuntime struct {
    scope containercontract.Scope
}

func (instance *substitutableRuntime) Context() context.Context {
    return context.Background()
}

func (instance *substitutableRuntime) Scope() containercontract.Scope {
    return instance.scope
}

func (instance *substitutableRuntime) Kind() containercontract.Kind {
    return containercontract.KindRequest
}

func newTypedNilScopeRuntime() containercontract.Runtime {
    return &substitutableRuntime{
        scope: (*testScope)(nil),
    }
}

func TestSecurityContextFromRuntime_AnswersNotFoundForATypedNilRuntime(t *testing.T) {
    var runtimeInstance containercontract.Runtime = (*substitutableRuntime)(nil)

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if true == exists {
        t.Fatalf("expected exists to be false for a typed-nil runtime")
    }

    if nil != securityContext {
        t.Fatalf("expected a nil security context for a typed-nil runtime")
    }
}

func TestSecurityContextFromRuntime_AnswersNotFoundForATypedNilScope(t *testing.T) {
    securityContext, exists := SecurityContextFromRuntime(newTypedNilScopeRuntime())
    if true == exists {
        t.Fatalf("expected exists to be false for a typed-nil scope")
    }

    if nil != securityContext {
        t.Fatalf("expected a nil security context for a typed-nil scope")
    }
}

func TestSecurityContextSetOnRuntime_TypedNilRuntimePanicsWithTheRefusal(t *testing.T) {
    var runtimeInstance containercontract.Runtime = (*substitutableRuntime)(nil)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            SecurityContextSetOnRuntime(
                runtimeInstance,
                NewSecurityContext(newTestCompiledFirewallWithRoleHierarchy("main", nil), nil),
            )
        },
        "runtime is nil for security context",
    )
}

func TestSecurityContextSetOnRuntime_TypedNilScopePanicsWithTheRefusal(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            SecurityContextSetOnRuntime(
                newTypedNilScopeRuntime(),
                NewSecurityContext(newTestCompiledFirewallWithRoleHierarchy("main", nil), nil),
            )
        },
        "runtime scope is nil for security context",
    )
}

func TestSecurityContextFromRuntime_DoesNotPanicWhenResolutionFails(t *testing.T) {
    scope := &erroringResolverScope{testScope: newTestScope()}
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    securityContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if true == exists {
        t.Fatalf("expected exists to be false when resolution fails")
    }
    if nil != securityContext {
        t.Fatalf("expected a nil security context when resolution fails")
    }
}

func TestSecurityContextSetOnRuntime_NilRuntimePanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            SecurityContextSetOnRuntime(
                nil,
                NewSecurityContext(newTestCompiledFirewallWithRoleHierarchy("main", nil), nil),
            )
        },
        "runtime is nil for security context",
    )
}

func TestSecurityContextSetOnRuntime_NilSecurityContextPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            SecurityContextSetOnRuntime(newTestRuntime(), nil)
        },
        "security context is nil for runtime",
    )
}

func TestSecurityContextFromRuntime_RefusesANilRuntime(t *testing.T) {
    securityContext, exists := SecurityContextFromRuntime(nil)
    if true == exists {
        t.Fatalf("expected exists to be false without a runtime")
    }

    if nil != securityContext {
        t.Fatalf("expected a nil security context without a runtime")
    }
}

func TestSecurityContextSetOnRuntime_RoundTripsThroughTheScope(t *testing.T) {
    runtimeInstance := newTestRuntime()

    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil),
    )

    SecurityContextSetOnRuntime(runtimeInstance, securityContext)

    readContext, exists := SecurityContextFromRuntime(runtimeInstance)
    if false == exists {
        t.Fatalf("expected the stored security context to be found")
    }

    if securityContext != readContext {
        t.Fatalf("expected the stored security context to be answered")
    }
}

func TestSecurityContextFromRuntime_AnswersNotFoundOnAnEmptyScope(t *testing.T) {
    securityContext, exists := SecurityContextFromRuntime(newTestRuntime())
    if true == exists {
        t.Fatalf("expected exists to be false on a scope without a security context")
    }

    if nil != securityContext {
        t.Fatalf("expected a nil security context on a scope without one")
    }
}

func TestFirewallManagerFromResolver_AnswersNilWhenUnregistered(t *testing.T) {
    if nil != FirewallManagerFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{})) {
        t.Fatalf("expected nil when no firewall manager is registered")
    }
}

func TestFirewallManagerFromResolver_AnswersTheRegisteredManager(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    manager := NewFirewallManager(NewCompiledConfiguration(nil, nil))

    registerErr := serviceContainer.Register(
        ServiceFirewallManager,
        func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
            return manager, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    if manager != FirewallManagerFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the registered firewall manager to be answered")
    }

    if manager != FirewallManagerMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the strict reader to answer the registered firewall manager")
    }
}

func TestFirewallManagerMustFromResolver_PanicsWhenUnregistered(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the strict reader to panic when no firewall manager is registered")
        }
    }()

    _ = FirewallManagerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}))
}

func TestFirewallManagerFromResolver_AnswersNilWhenUnregisteredThroughAScope(t *testing.T) {
    if nil != FirewallManagerFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope()) {
        t.Fatalf("expected nil when no firewall manager is registered")
    }
}

func TestFirewallManagerFromResolver_AnswersTheRegisteredManagerThroughAScope(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    manager := NewFirewallManager(NewCompiledConfiguration(nil, nil))

    registerErr := serviceContainer.Register(
        ServiceFirewallManager,
        func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
            return manager, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    scope := serviceContainer.NewScope()

    if manager != FirewallManagerFromResolver(t.Context(), scope) {
        t.Fatalf("expected the registered firewall manager to be answered through the scope")
    }

    if manager != FirewallManagerMustFromResolver(t.Context(), scope) {
        t.Fatalf("expected the strict reader to answer the registered firewall manager through the scope")
    }
}

func TestFirewallManagerMustFromResolver_PanicsWhenUnregisteredThroughAScope(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the strict reader to panic when no firewall manager is registered")
        }
    }()

    _ = FirewallManagerMustFromResolver(t.Context(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

func TestSecurityContextSetOnRuntime_ABootRuntimeIsRefused(t *testing.T) {
    bootRuntime, finish := runtime.NewBoot(t.Context(), container.NewContainer(container.ContainerConfig{}))
    defer func() {
        _ = finish()
    }()

    testhelper.AssertPanicsWithError(t, func() {
        SecurityContextSetOnRuntime(bootRuntime, &SecurityContext{})
    }, "the security context is set in a request or a run")
}
