package config

import (
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v3"
    "github.com/precision-soft/melody/v3/exception"
    tracenoop "go.opentelemetry.io/otel/trace/noop"
)

/* buildObservability builds one prometheus meter and hands it to both instrument families: the metrics middleware counts the requests the pipeline handles (http.server.request.*), and the lifecycle decorator, wrapped around the whole kernel handler, counts every request (http.server.lifecycle.request.*) — the security refusals and the other short-circuits the middleware never sees included. The decorator is wired for its metrics: its tracer is a no-op, because the spans are the tracing middleware's, which the otlp module installs with its own provider, and a lifecycle span beside it would put a second span on every trace. The no-op span keeps the extracted trace context, so the middleware's span stays under the caller's trace. */
func (instance *Module) buildObservability() {
    meter, registry, meterErr := melodyopentelemetry.NewPrometheusMeter("melody.example")
    if nil != meterErr {
        exception.Panic(exception.FromError(meterErr))
    }

    middleware, middlewareErr := melodyopentelemetry.NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        exception.Panic(exception.FromError(middlewareErr))
    }

    decorator, decoratorErr := melodyopentelemetry.NewHandlerDecorator(melodyopentelemetry.HandlerDecoratorConfig{
        Tracer: tracenoop.NewTracerProvider().Tracer("melody.example"),
        Meter:  meter,
    })
    if nil != decoratorErr {
        exception.Panic(exception.FromError(decoratorErr))
    }

    instance.metricsMiddleware = middleware
    instance.metricsHandler = melodyopentelemetry.MetricsHandler(registry)
    instance.lifecycleDecorator = decorator
}

/* buildMetricsAuth reads the credential the metrics scraper presents. The exposition names every route and its traffic, so with a token set /metrics answers only a caller presenting it; an empty value leaves the door public, the development default of a stack without a scraper. */
func (instance *Module) buildMetricsAuth() {
    instance.metricsToken = instance.environmentValue(environmentKeyMetricsToken)
}
