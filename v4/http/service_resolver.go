package http

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

const (
    ServiceRouteRegistry  = "service.http.route.registry"
    ServiceUrlGenerator   = "service.http.url.generator"
    ServiceRouter         = "service.http.router"
    ServiceRequestContext = "service.http.request.context"
)

func RouteRegistryMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.RouteRegistry {
    return container.MustFromResolver[httpcontract.RouteRegistry](ctx, resolver, ServiceRouteRegistry)
}

func UrlGeneratorMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.UrlGenerator {
    return container.MustFromResolver[httpcontract.UrlGenerator](ctx, resolver, ServiceUrlGenerator)
}

func RouterMustFromResolver(ctx context.Context, resolver containercontract.Resolver) httpcontract.Router {
    return container.MustFromResolver[httpcontract.Router](ctx, resolver, ServiceRouter)
}

/* RequestContextFromRuntime answers the request's context, its id and start moment; a runtime that is not a request's answers an error under containercontract.ErrRuntimeKindRefused naming both kinds. */
func RequestContextFromRuntime(runtimeInstance containercontract.Runtime) (httpcontract.RequestContext, error) {
    if kindErr := containercontract.RefuseRuntimeKind(runtimeInstance, "RequestContextFromRuntime", []containercontract.Kind{containercontract.KindRequest}); nil != kindErr {
        return nil, kindErr
    }

    return container.FromResolver[httpcontract.RequestContext](runtimeInstance.Context(), runtimeInstance.Scope(), ServiceRequestContext)
}

func RequestContextMustFromRuntime(runtimeInstance containercontract.Runtime) httpcontract.RequestContext {
    requestContext, err := RequestContextFromRuntime(runtimeInstance)
    if nil != err {
        exception.Panic(exception.NewError("could not resolve the request context", nil, err))
    }

    return requestContext
}
