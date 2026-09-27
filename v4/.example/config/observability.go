package config

import (
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v4"
    "github.com/precision-soft/melody/v4/exception"
)

func (instance *Module) buildObservability() {
    middleware, handler, buildErr := melodyopentelemetry.NewMetricsMiddlewareWithPrometheus("melody.example")
    if nil != buildErr {
        exception.Panic(exception.FromError(buildErr))
    }

    instance.metricsMiddleware = middleware
    instance.metricsHandler = handler
}
