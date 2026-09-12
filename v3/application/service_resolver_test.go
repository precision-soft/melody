package application

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* the process role is what a background worker consults before it starts doing work — a web process that read it as "worker" would run the cron dispatch of every replica — and the service NAME is the contract every such caller reaches it by. Neither door had ever been called. */
func TestProcessRoleMustFromContainerAndResolver_ResolveTheDeclaredServiceName(t *testing.T) {
    if "service.application.process_role" != ServiceProcessRole {
        t.Fatalf("the process role service name is a cross-package contract, got %q", ServiceProcessRole)
    }

    serviceContainer := container.NewContainer()

    registerErr := serviceContainer.Register(
        ServiceProcessRole,
        func(resolver containercontract.Resolver) (string, error) {
            return config.RoleWorker, nil
        },
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if config.RoleWorker != ProcessRoleMustFromContainer(serviceContainer) {
        t.Fatalf("expected the container door to resolve the declared role")
    }

    if config.RoleWorker != ProcessRoleMustFromResolver(serviceContainer) {
        t.Fatalf("expected the resolver door to resolve the declared role")
    }
}

/* both doors are the panicking form: a service gating background work on a role that failed to resolve would fall back to the empty string, which matches no role and would silently disable the work it guards. It has to fail at the line that asked. */
func TestProcessRoleMustFromResolver_PanicsWhenTheRoleIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing process role to panic")
        }
    }()

    _ = ProcessRoleMustFromResolver(serviceContainer)
}

/* the process context lives only on a console run's scope — the cli entry point installs it and the root container never carries it — so its accessor takes a resolver, the same door the request context takes on the http side */
func TestProcessContextMustFromResolver_ResolvesTheInstalledContext(t *testing.T) {
    if "service.application.process_context" != ServiceProcessContext {
        t.Fatalf("the process context service name is a cross-package contract, got %q", ServiceProcessContext)
    }

    serviceContainer := container.NewContainer()

    runScope := serviceContainer.NewScope()
    defer runScope.Close()

    startedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
    installed := NewProcessContext("process-1", startedAt)

    if overrideErr := runScope.OverrideProtectedInstance(ServiceProcessContext, installed); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    resolved := ProcessContextMustFromResolver(runScope)

    if "process-1" != resolved.ProcessId() {
        t.Fatalf("expected the installed process id, got %q", resolved.ProcessId())
    }

    if false == startedAt.Equal(resolved.StartedAt()) {
        t.Fatalf("expected the installed start moment, got %v", resolved.StartedAt())
    }

    tolerantAnswer := ProcessContextFromResolver(runScope)
    if nil == tolerantAnswer || "process-1" != tolerantAnswer.ProcessId() {
        t.Fatalf("expected the tolerant form to answer the installed context, got %v", tolerantAnswer)
    }
}

/* absence — an http process, the root container — answers nil from the tolerant form, so code shared between the two process shapes can treat the process context as optional without recovering a panic */
func TestProcessContextFromResolver_AnswersNilWhenAbsent(t *testing.T) {
    serviceContainer := container.NewContainer()

    runScope := serviceContainer.NewScope()
    defer runScope.Close()

    if nil != ProcessContextFromResolver(runScope) {
        t.Fatalf("expected nil for a scope without the process context")
    }

    if nil != ProcessContextFromResolver(serviceContainer) {
        t.Fatalf("expected nil for the root container")
    }
}
