package http

import (
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
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

/* RequestContextMustFromResolver resolves the current request's context, its id and start moment. It takes a resolver, since the kernel installs the service on each request scope only: resolved from the root container or a console process it panics with the service not registered. */
func RequestContextMustFromResolver(resolver containercontract.Resolver) httpcontract.RequestContext {
    return container.MustFromResolver[httpcontract.RequestContext](resolver, ServiceRequestContext)
}

/* RequestContextFromResolver is the error-tolerant form of RequestContextMustFromResolver: absent, as on the root container or in a console process, it answers nil. */
func RequestContextFromResolver(resolver containercontract.Resolver) httpcontract.RequestContext {
    requestContextInstance, err := container.FromResolver[httpcontract.RequestContext](resolver, ServiceRequestContext)
    if nil != err || true == internal.IsNilInterface(requestContextInstance) {
        return nil
    }

    return requestContextInstance
}
