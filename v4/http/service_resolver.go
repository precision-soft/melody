package http

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

const (
    ServiceRouteRegistry  = "service.http.route_registry"
    ServiceUrlGenerator   = "service.http.url_generator"
    ServiceRouter         = "service.http.router"
    ServiceRequestContext = "service.http.request_context"
)

func RouteRegistryMustFromRuntime(runtimeInstance containercontract.Runtime) httpcontract.RouteRegistry {
    return runtime.MustFromRuntime[httpcontract.RouteRegistry](runtimeInstance, ServiceRouteRegistry)
}

func RouteRegistryMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.RouteRegistry {
    return container.MustFromResolver[httpcontract.RouteRegistry](ctx, resolver, ServiceRouteRegistry)
}

func UrlGeneratorMustFromRuntime(runtimeInstance containercontract.Runtime) httpcontract.UrlGenerator {
    return runtime.MustFromRuntime[httpcontract.UrlGenerator](runtimeInstance, ServiceUrlGenerator)
}

func UrlGeneratorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.UrlGenerator {
    return container.MustFromResolver[httpcontract.UrlGenerator](ctx, resolver, ServiceUrlGenerator)
}

func RouterMustFromRuntime(runtimeInstance containercontract.Runtime) httpcontract.Router {
    return runtime.MustFromRuntime[httpcontract.Router](runtimeInstance, ServiceRouter)
}

func RouterMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.Router {
    return container.MustFromResolver[httpcontract.Router](ctx, resolver, ServiceRouter)
}

/* RequestContextFromRuntime answers the request's context, its id and start moment; a runtime that is not a request's answers an error under containercontract.ErrRuntimeKindRefused naming both kinds. */
func RequestContextFromRuntime(runtimeInstance containercontract.Runtime) (httpcontract.RequestContext, error) {
    if kindErr := containercontract.RefuseRuntimeKind(runtimeInstance, "RequestContextFromRuntime", []containercontract.Kind{containercontract.KindRequest}); nil != kindErr {
        return nil, kindErr
    }

    return runtime.FromRuntime[httpcontract.RequestContext](runtimeInstance, ServiceRequestContext)
}

func RequestContextMustFromRuntime(runtimeInstance containercontract.Runtime) httpcontract.RequestContext {
    requestContext, err := RequestContextFromRuntime(runtimeInstance)
    if nil != err {
        exception.Panic(exception.NewError("could not resolve the request context", nil, err))
    }

    return requestContext
}
