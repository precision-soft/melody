package config

import (
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *Module) buildObservability() {
    middleware, handler, buildErr := melodyopentelemetry.NewMetricsMiddlewareWithPrometheus("melody.example")
    if nil != buildErr {
        exception.Panic(exception.FromError(buildErr))
    }

    instance.metricsMiddleware = middleware
    instance.metricsHandler = handler
}
