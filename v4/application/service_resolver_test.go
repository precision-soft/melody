package application

import (
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
)

/* the process role is what a background worker consults before it starts doing work — a web process that read it as "worker" would run the cron dispatch of every replica — and the service NAME is the contract every such caller reaches it by. */
func TestProcessRoleMustFromResolver_ResolveTheDeclaredServiceName(t *testing.T) {
    if "service.application.process_role" != ServiceProcessRole {
        t.Fatalf("the process role service name is a cross-package contract, got %q", ServiceProcessRole)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registerErr := serviceContainer.Register(
        ServiceProcessRole,
        func(resolver containercontract.Resolver) (string, error) {
            return config.RoleWorker, nil
        },
        container.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if config.RoleWorker != ProcessRoleMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the container door to resolve the declared role")
    }

    if config.RoleWorker != ProcessRoleMustFromResolver(t.Context(), serviceContainer) {
        t.Fatalf("expected the resolver door to resolve the declared role")
    }
}

/* both doors are the panicking form: a service gating background work on a role that failed to resolve would fall back to the empty string, which matches no role and would silently disable the work it guards. It has to fail at the line that asked. */
func TestProcessRoleMustFromResolver_PanicsWhenTheRoleIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing process role to panic")
        }
    }()

    _ = ProcessRoleMustFromResolver(t.Context(), serviceContainer)
}

func TestProcessContextFromRuntime_ResolvesTheInstalledContext(t *testing.T) {
    if "service.application.process_context" != ServiceProcessContext {
        t.Fatalf("the process context service name is a cross-package contract, got %q", ServiceProcessContext)
    }

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    runScope := serviceContainer.NewScope()
    defer runScope.Close()

    startedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
    installed := NewProcessContext("process-1", startedAt)

    if overrideErr := runScope.OverrideProtectedInstance(ServiceProcessContext, installed, container.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    resolved := ProcessContextMustFromRuntime(runtime.NewRun(t.Context(), runScope))

    if "process-1" != resolved.ProcessId() {
        t.Fatalf("expected the installed process id, got %q", resolved.ProcessId())
    }

    if false == startedAt.Equal(resolved.StartedAt()) {
        t.Fatalf("expected the installed start moment, got %v", resolved.StartedAt())
    }
}

func TestProcessContextFromRuntime_ARequestRuntimeIsRefusedNamingBothKinds(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    _, lookupErr := ProcessContextFromRuntime(runtime.NewRequest(t.Context(), serviceContainer.NewScope()))
    if false == errors.Is(lookupErr, containercontract.ErrRuntimeKindRefused) {
        t.Fatalf("expected a request runtime to be refused, got %v", lookupErr)
    }

    var refusal *exception.Error
    if false == errors.As(lookupErr, &refusal) || "run" != refusal.Context()["required"] || "request" != refusal.Context()["given"] {
        t.Fatalf("expected the refusal to name both kinds, got %v", lookupErr)
    }
}
