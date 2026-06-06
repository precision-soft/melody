package config

import (
    nethttp "net/http"

    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    "github.com/precision-soft/melody/v3/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func (instance *Module) buildObservability() {
    middleware, handler, buildErr := melodyopentelemetry.NewMetricsMiddlewareWithPrometheus("melody.example")
    if nil != buildErr {
        exception.Panic(exception.FromError(buildErr))
    }

    instance.metricsMiddleware = middleware
    instance.metricsHandler = handler
}

func metricsRouteHandler(handler nethttp.Handler) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        handler.ServeHTTP(writer, request.HttpRequest())

        return nil, nil
    }
}
