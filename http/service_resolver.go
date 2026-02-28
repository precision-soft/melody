package http

import (
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
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
