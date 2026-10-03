package security

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* erroringResolverScope answers Has affirmatively but fails every Get, modelling the window in which the security context is present when Has runs and the scope is closed by the time Get runs. */
type erroringResolverScope struct {
    *testScope
}

func (instance *erroringResolverScope) Has(serviceName string) bool {
    return true
}

func (instance *erroringResolverScope) Get(serviceName string) (any, error) {
    return nil, errors.New("scope is closed")
}

/* substitutableRuntime stands for a Runtime supplied by application code rather than built by runtime.New, which is the only shape that can carry a typed-nil scope past construction. */
type substitutableRuntime struct {
    scope            containercontract.Scope
    serviceContainer containercontract.Container
}

func (instance *substitutableRuntime) Context() context.Context {
    return context.Background()
}

func (instance *substitutableRuntime) Scope() containercontract.Scope {
    return instance.scope
}

func (instance *substitutableRuntime) Container() containercontract.Container {
    return instance.serviceContainer
}

func newTypedNilScopeRuntime() runtimecontract.Runtime {
    return &substitutableRuntime{
        scope:            (*testScope)(nil),
        serviceContainer: container.NewContainer(),
    }
}

func TestSecurityContextFromRuntime_AnswersNotFoundForATypedNilRuntime(t *testing.T) {
    var runtimeInstance runtimecontract.Runtime = (*substitutableRuntime)(nil)

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
    var runtimeInstance runtimecontract.Runtime = (*substitutableRuntime)(nil)

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

/* SecurityContextFromRuntime runs from IsGranted, which a handler can call from a goroutine that outlives the request; resolving the logger with the panicking Must variant on a closed scope crashes that uncovered goroutine, so a failed resolution must return (nil, false) rather than panic */
func TestSecurityContextFromRuntime_DoesNotPanicWhenResolutionFails(t *testing.T) {
    scope := &erroringResolverScope{testScope: newTestScope()}
    runtimeInstance := runtime.New(context.Background(), scope, container.NewContainer())

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

/* the two refusals are told apart by message: storing a nil context would put a nil where every reader expects a context, and the failure would surface as a dereference in a handler rather than at the wiring mistake */
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

/* the ordinary round trip: what was stored on the runtime is what comes back */
func TestSecurityContextSetOnRuntime_RoundTripsThroughTheScope(t *testing.T) {
    runtimeInstance := newTestRuntime()

    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        NewAuthenticatedToken("u1", []string{"ROLE_USER"}),
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

/* a scope that never carried a security context answers not-found rather than resolving nothing into a typed nil */
func TestSecurityContextFromRuntime_AnswersNotFoundOnAnEmptyScope(t *testing.T) {
    securityContext, exists := SecurityContextFromRuntime(newTestRuntime())
    if true == exists {
        t.Fatalf("expected exists to be false on a scope without a security context")
    }

    if nil != securityContext {
        t.Fatalf("expected a nil security context on a scope without one")
    }
}

/* the soft container reader answers nil rather than panicking when the firewall manager was never registered, which is what lets a caller decide instead of dying */
func TestFirewallManagerFromContainer_AnswersNilWhenUnregistered(t *testing.T) {
    if nil != FirewallManagerFromContainer(container.NewContainer()) {
        t.Fatalf("expected nil when no firewall manager is registered")
    }
}

func TestFirewallManagerFromContainer_AnswersTheRegisteredManager(t *testing.T) {
    serviceContainer := container.NewContainer()

    manager := NewFirewallManager(NewCompiledConfiguration(nil, nil))

    registerErr := serviceContainer.Register(
        ServiceFirewallManager,
        func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
            return manager, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    if manager != FirewallManagerFromContainer(serviceContainer) {
        t.Fatalf("expected the registered firewall manager to be answered")
    }

    if manager != FirewallManagerMustFromContainer(serviceContainer) {
        t.Fatalf("expected the strict reader to answer the registered firewall manager")
    }
}

/* the strict reader is the boot-time one: it panics rather than handing back a nil the caller would dereference later */
func TestFirewallManagerMustFromContainer_PanicsWhenUnregistered(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the strict reader to panic when no firewall manager is registered")
        }
    }()

    _ = FirewallManagerMustFromContainer(container.NewContainer())
}

/* the Resolver-taking pair exists only on v3, where a scoped service reads the firewall manager through the scope it was built in rather than through the container: the container doors above answer a Container, and a Scope is not one. */
func TestFirewallManagerFromResolver_AnswersNilWhenUnregistered(t *testing.T) {
    if nil != FirewallManagerFromResolver(container.NewContainer().NewScope()) {
        t.Fatalf("expected nil when no firewall manager is registered")
    }
}

func TestFirewallManagerFromResolver_AnswersTheRegisteredManagerThroughAScope(t *testing.T) {
    serviceContainer := container.NewContainer()

    manager := NewFirewallManager(NewCompiledConfiguration(nil, nil))

    registerErr := serviceContainer.Register(
        ServiceFirewallManager,
        func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
            return manager, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected error: %v", registerErr)
    }

    scope := serviceContainer.NewScope()

    if manager != FirewallManagerFromResolver(scope) {
        t.Fatalf("expected the registered firewall manager to be answered through the scope")
    }

    if manager != FirewallManagerMustFromResolver(scope) {
        t.Fatalf("expected the strict reader to answer the registered firewall manager through the scope")
    }
}

/* the strict reader is the boot-time one: it panics rather than handing back a nil the caller would dereference later */
func TestFirewallManagerMustFromResolver_PanicsWhenUnregistered(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected the strict reader to panic when no firewall manager is registered")
        }
    }()

    _ = FirewallManagerMustFromResolver(container.NewContainer().NewScope())
}
