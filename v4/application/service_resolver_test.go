package application

import (
    "errors"
    "regexp"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/lock"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/mailer"
    "github.com/precision-soft/melody/v4/messagebus"
    "github.com/precision-soft/melody/v4/openapi"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/serializer"
    "github.com/precision-soft/melody/v4/session"
    "github.com/precision-soft/melody/v4/storage"
    "github.com/precision-soft/melody/v4/translation"
    "github.com/precision-soft/melody/v4/validation"
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

/* coreServiceNames is kept by hand: 32 names, every Service* constant the core declares, so a constant added beside them is caught by whoever reviews this count, not by the compiler. */
var coreServiceNames = []string{
    logging.ServiceLogger,
    validation.ServiceValidator,
    event.ServiceEventDispatcher,
    clock.ServiceClock,
    storage.ServiceStorage,
    lock.ServiceLocker,
    translation.ServiceTranslator,
    config.ServiceConfig,
    serializer.ServiceSerializer,
    serializer.ServiceSerializerManager,
    cache.ServiceCache,
    cache.ServiceCacheBackend,
    cache.ServiceCacheSerializer,
    messagebus.ServiceBus,
    messagebus.ServiceConsumeBus,
    messagebus.ServiceHandlerLocator,
    messagebus.ServiceTransports,
    messagebus.ServiceTransportsCloser,
    security.ServiceFirewallManager,
    securitycontract.ServiceSecurityContext,
    ServiceProcessRole,
    ServiceProcessContext,
    session.ServiceSessionManager,
    session.ServiceSessionStorage,
    openapi.ServiceOpenApiRegistry,
    openapi.ServiceOpenApiInfo,
    mailer.ServiceMailer,
    http.ServiceRouteRegistry,
    http.ServiceUrlGenerator,
    http.ServiceRouter,
    http.ServiceRequestContext,
}

func TestServiceNames_OfTheCoreMatchOnePattern(t *testing.T) {
    if 31 != len(coreServiceNames) {
        t.Fatalf("expected the 31 core names the list states, got %d", len(coreServiceNames))
    }

    grammar := regexp.MustCompile(`^service\.[a-z0-9]+(\.[a-z0-9_]+)?$`)
    seen := make(map[string]bool, len(coreServiceNames))
    for _, serviceName := range coreServiceNames {
        if false == grammar.MatchString(serviceName) {
            t.Fatalf("expected %q to read service.<module>.<snake_case_name>", serviceName)
        }

        if true == seen[serviceName] {
            t.Fatalf("expected %q named once", serviceName)
        }
        seen[serviceName] = true
    }
}
