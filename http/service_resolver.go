package http

import (
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
)

const (
    ServiceRouteRegistry  = "service.http.route.registry"
    ServiceUrlGenerator   = "service.http.url.generator"
    ServiceRouter         = "service.http.router"
    ServiceRequestContext = "service.http.request.context"
)

func RouteRegistryMustFromContainer(serviceContainer containercontract.Container) httpcontract.RouteRegistry {
    return container.MustFromResolver[httpcontract.RouteRegistry](serviceContainer, ServiceRouteRegistry)
}

func UrlGeneratorMustFromContainer(serviceContainer containercontract.Container) httpcontract.UrlGenerator {
    return container.MustFromResolver[httpcontract.UrlGenerator](serviceContainer, ServiceUrlGenerator)
}

func RouterMustFromContainer(serviceContainer containercontract.Container) httpcontract.Router {
    return container.MustFromResolver[httpcontract.Router](serviceContainer, ServiceRouter)
}

/* RequestContextMustFromResolver resolves the current request's context — its id and start moment. Unlike its three siblings above, it takes a resolver rather than the container, because the service exists only on a request scope: the kernel installs it into the scope of each request, the root container never carries it, and a console process has application.ServiceProcessContext instead. A scoped provider hands its own resolver here; resolving from the container, or from a console process, panics with the service not registered. */
func RequestContextMustFromResolver(resolver containercontract.Resolver) httpcontract.RequestContext {
    return container.MustFromResolver[httpcontract.RequestContext](resolver, ServiceRequestContext)
}

/* RequestContextFromResolver is the error-tolerant form of RequestContextMustFromResolver, for code that runs on both process shapes and treats the request context as optional: absence — a console process, the root container — answers nil. */
func RequestContextFromResolver(resolver containercontract.Resolver) httpcontract.RequestContext {
    requestContextInstance, err := container.FromResolver[httpcontract.RequestContext](resolver, ServiceRequestContext)
    if nil != err || true == internal.IsNilInterface(requestContextInstance) {
        return nil
    }

    return requestContextInstance
}
