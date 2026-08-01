package http

import (
    "testing"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
)

/* @info the three Must resolvers are how the framework and an application reach the routing services out of the container; two of them had never been entered. Each is bound to a service name, and a resolver reading the wrong name would hand back a service of the wrong kind — the failure surfaces as a type assertion deep inside url generation rather than at the wiring mistake. */

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

/* @info the three service names are distinct constants: two resolvers sharing one name would silently hand the same service to both callers, and the mistake would only appear where the returned value is used. */

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
