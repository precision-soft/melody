package http

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* the three Must resolvers are how the framework and an application reach the routing services out of the container. Each is bound to a service name, and a resolver reading the wrong name would hand back a service of the wrong kind — the failure surfaces as a type assertion deep inside url generation rather than at the wiring mistake. */

func newRoutingServiceContainer() (containercontract.Container, *RouteRegistry, *Router) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    serviceContainer.MustRegister(
        ServiceRouteRegistry,
        func(resolver containercontract.Resolver) (httpcontract.RouteRegistry, error) {
            return routeRegistry, nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        ServiceUrlGenerator,
        func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
            return NewUrlGenerator(routeRegistry, UrlGeneratorConfig{}), nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
        container.RegisterOptions{},
    )

    return serviceContainer, routeRegistry, router
}

func TestRouteRegistryMustFromResolver_ResolvesTheRegisteredRegistry(t *testing.T) {
    serviceContainer, routeRegistry, _ := newRoutingServiceContainer()

    resolved := RouteRegistryMustFromResolver(t.Context(), serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the registry to resolve")
    }

    if routeRegistry != resolved {
        t.Fatalf("expected the resolver to hand back the registered registry")
    }
}

func TestUrlGeneratorMustFromResolver_ResolvesTheRegisteredGenerator(t *testing.T) {
    serviceContainer, _, router := newRoutingServiceContainer()

    router.Handle("/articles/:id", routeRegistryTestHandler(), RouteOptions{Name: "article.show", Methods: []string{"GET"}})

    resolved := UrlGeneratorMustFromResolver(t.Context(), serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the url generator to resolve")
    }

    generatedPath, generateErr := resolved.GeneratePath("article.show", httpcontract.PathConfig{Parameters: map[string]string{"id": "42"}})
    if nil != generateErr {
        t.Fatalf("unexpected generation error: %v", generateErr)
    }

    if "/articles/42" != generatedPath {
        t.Fatalf("expected the resolved generator to read the same registry as the router, got: %q", generatedPath)
    }
}

func TestRouterMustFromResolver_ResolvesTheRegisteredRouter(t *testing.T) {
    serviceContainer, _, router := newRoutingServiceContainer()

    resolved := RouterMustFromResolver(t.Context(), serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the router to resolve")
    }

    if router != resolved {
        t.Fatalf("expected the resolver to hand back the registered router")
    }
}

func TestRouterMustFromResolver_ResolvesTheRegisteredRouterThroughAScope(t *testing.T) {
    serviceContainer, _, router := newRoutingServiceContainer()

    if router != RouterMustFromResolver(t.Context(), serviceContainer.NewScope()) {
        t.Fatalf("expected a scope to resolve the container's router")
    }
}

/* the three service names are distinct constants: two resolvers sharing one name would silently hand the same service to both callers, and the mistake would only appear where the returned value is used. */

func TestRoutingServiceNames_AreDistinct(t *testing.T) {
    names := map[string]string{
        ServiceRouteRegistry:  "route registry",
        ServiceUrlGenerator:   "url generator",
        ServiceRouter:         "router",
        ServiceRequestContext: "request context",
    }

    if 4 != len(names) {
        t.Fatalf("expected four distinct routing service names, got: %v", names)
    }
}

func TestRequestContextFromRuntime_ResolvesTheInstalledContext(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    requestScope := serviceContainer.NewScope()
    defer requestScope.CloseWithContext(context.Background())

    startedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
    installed := NewRequestContext("request-1", startedAt)

    if overrideErr := requestScope.OverrideProtectedInstance(ServiceRequestContext, installed, container.OverrideOptions{}); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    resolved := RequestContextMustFromRuntime(runtime.NewRequest(t.Context(), requestScope))

    if "request-1" != resolved.RequestId() {
        t.Fatalf("expected the installed request id, got %q", resolved.RequestId())
    }

    if false == startedAt.Equal(resolved.StartedAt()) {
        t.Fatalf("expected the installed start moment, got %v", resolved.StartedAt())
    }
}

func TestRequestContextFromRuntime_ARuntimeOfAnotherKindIsRefusedNamingBothKinds(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    bootRuntime, finish := runtime.NewBoot(t.Context(), serviceContainer)
    defer func() {
        _ = finish()
    }()

    for _, runtimeInstance := range []containercontract.Runtime{runtime.NewRun(t.Context(), serviceContainer.NewScope()), bootRuntime} {
        _, lookupErr := RequestContextFromRuntime(runtimeInstance)
        if false == errors.Is(lookupErr, containercontract.ErrRuntimeKindRefused) {
            t.Fatalf("expected a %s runtime to be refused, got %v", runtimeInstance.Kind(), lookupErr)
        }

        var refusal *exception.Error
        if false == errors.As(lookupErr, &refusal) || "request" != refusal.Context()["required"] || runtimeInstance.Kind().String() != refusal.Context()["given"] {
            t.Fatalf("expected the refusal to name both kinds, got %v", lookupErr)
        }
    }
}

func TestServiceNames_FollowTheGrammar(t *testing.T) {
    for serviceName, expected := range map[string]string{
        ServiceRouteRegistry:  "service.http.route_registry",
        ServiceUrlGenerator:   "service.http.url_generator",
        ServiceRequestContext: "service.http.request_context",
        ServiceRouter:         "service.http.router",
    } {
        if expected != serviceName {
            t.Fatalf("expected %q, got %q", expected, serviceName)
        }
    }
}
