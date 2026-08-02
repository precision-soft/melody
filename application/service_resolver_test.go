package application

import (
    "testing"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
)

/* @info the process role is what a background worker consults before it starts doing work — a web process that read it as "worker" would run the cron dispatch of every replica — and the service NAME is the contract every such caller reaches it by. Neither door had ever been called. */
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

/* @info both doors are the panicking form: a service gating background work on a role that failed to resolve would fall back to the empty string, which matches no role and would silently disable the work it guards. It has to fail at the line that asked. */
func TestProcessRoleMustFromResolver_PanicsWhenTheRoleIsNotRegistered(t *testing.T) {
    serviceContainer := container.NewContainer()

    defer func() {
        if nil == recover() {
            t.Fatalf("expected the missing process role to panic")
        }
    }()

    _ = ProcessRoleMustFromResolver(serviceContainer)
}
