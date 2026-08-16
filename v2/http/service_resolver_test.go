package http

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
)

/* the three Must resolvers are how the framework and an application reach the routing services out of the container; two of them had never been entered. Each is bound to a service name, and a resolver reading the wrong name would hand back a service of the wrong kind — the failure surfaces as a type assertion deep inside url generation rather than at the wiring mistake. */

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

/* the request context lives only on a request scope — the kernel installs it per request and the root container never carries it — so its accessor takes a resolver: the door the documentation's scoped-provider example already used before the function existed */
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

/* absence — a console process, the root container — answers nil from the tolerant form, so code shared between the two process shapes can treat the request context as optional without recovering a panic */
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
