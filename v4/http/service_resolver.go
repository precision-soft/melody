package http

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    ServiceRouteRegistry  = "service.http.route.registry"
    ServiceUrlGenerator   = "service.http.url.generator"
    ServiceRouter         = "service.http.router"
    ServiceRequestContext = "service.http.request.context"
)

func RouteRegistryMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) httpcontract.RouteRegistry {
    return container.MustFromResolver[httpcontract.RouteRegistry](ctx, serviceContainer, ServiceRouteRegistry)
}

func UrlGeneratorMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) httpcontract.UrlGenerator {
    return container.MustFromResolver[httpcontract.UrlGenerator](ctx, serviceContainer, ServiceUrlGenerator)
}

func RouterMustFromContainer(ctx context.Context, serviceContainer containercontract.Container) httpcontract.Router {
    return container.MustFromResolver[httpcontract.Router](ctx, serviceContainer, ServiceRouter)
}

/* RequestContextMustFromResolver resolves the current request's context, its id and start moment. It takes a resolver, since the kernel installs the service on each request scope only: resolved from the root container or a console process it panics with the service not registered. */
func RequestContextMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.RequestContext {
    return container.MustFromResolver[httpcontract.RequestContext](ctx, resolver, ServiceRequestContext)
}

/* RequestContextFromResolver is the error-tolerant form of RequestContextMustFromResolver: absent, as on the root container or in a console process, it answers nil. */
func RequestContextFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.RequestContext {
    requestContextInstance, err := container.FromResolver[httpcontract.RequestContext](ctx, resolver, ServiceRequestContext)
    if nil != err || true == internal.IsNilInterface(requestContextInstance) {
        return nil
    }

    return requestContextInstance
}
