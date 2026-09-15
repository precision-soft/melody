package http

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)


func newRoutingServiceContainer() (containercontract.Container, *RouteRegistry, *Router) {
    serviceContainer := container.NewContainer()

    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    serviceContainer.MustRegister(
        ServiceRouteRegistry,
        func(resolver containercontract.Resolver) (httpcontract.RouteRegistry, error) {
            return routeRegistry, nil
        },
    )

    serviceContainer.MustRegister(
        ServiceUrlGenerator,
        func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
            return NewUrlGenerator(routeRegistry), nil
        },
    )

    serviceContainer.MustRegister(
        ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    return serviceContainer, routeRegistry, router
}

func TestRouteRegistryMustFromContainer_ResolvesTheRegisteredRegistry(t *testing.T) {
    serviceContainer, routeRegistry, _ := newRoutingServiceContainer()

    resolved := RouteRegistryMustFromContainer(serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the registry to resolve")
    }

    if routeRegistry != resolved {
        t.Fatalf("expected the resolver to hand back the registered registry")
    }
}

func TestUrlGeneratorMustFromContainer_ResolvesTheRegisteredGenerator(t *testing.T) {
    serviceContainer, _, router := newRoutingServiceContainer()

    router.HandleNamed("article.show", "GET", "/articles/:id", routeRegistryTestHandler())

    resolved := UrlGeneratorMustFromContainer(serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the url generator to resolve")
    }

    generatedPath, generateErr := resolved.GeneratePath("article.show", map[string]string{"id": "42"})
    if nil != generateErr {
        t.Fatalf("unexpected generation error: %v", generateErr)
    }

    if "/articles/42" != generatedPath {
        t.Fatalf("expected the resolved generator to read the same registry as the router, got: %q", generatedPath)
    }
}

func TestRouterMustFromContainer_ResolvesTheRegisteredRouter(t *testing.T) {
    serviceContainer, _, router := newRoutingServiceContainer()

    resolved := RouterMustFromContainer(serviceContainer)

    if nil == resolved {
        t.Fatalf("expected the router to resolve")
    }

    if router != resolved {
        t.Fatalf("expected the resolver to hand back the registered router")
    }
}


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

func TestRequestContextMustFromResolver_ResolvesTheInstalledContext(t *testing.T) {
    serviceContainer := container.NewContainer()

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    startedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
    installed := NewRequestContext("request-1", startedAt)

    if overrideErr := requestScope.OverrideProtectedInstance(ServiceRequestContext, installed); nil != overrideErr {
        t.Fatalf("unexpected override error: %v", overrideErr)
    }

    resolved := RequestContextMustFromResolver(requestScope)

    if "request-1" != resolved.RequestId() {
        t.Fatalf("expected the installed request id, got %q", resolved.RequestId())
    }

    if false == startedAt.Equal(resolved.StartedAt()) {
        t.Fatalf("expected the installed start moment, got %v", resolved.StartedAt())
    }

    tolerantAnswer := RequestContextFromResolver(requestScope)
    if nil == tolerantAnswer || "request-1" != tolerantAnswer.RequestId() {
        t.Fatalf("expected the tolerant form to answer the installed context, got %v", tolerantAnswer)
    }
}

func TestRequestContextFromResolver_AnswersNilWhenAbsent(t *testing.T) {
    serviceContainer := container.NewContainer()

    requestScope := serviceContainer.NewScope()
    defer requestScope.Close()

    if nil != RequestContextFromResolver(requestScope) {
        t.Fatalf("expected nil for a scope without the request context")
    }

    if nil != RequestContextFromResolver(serviceContainer) {
        t.Fatalf("expected nil for the root container")
    }
}
