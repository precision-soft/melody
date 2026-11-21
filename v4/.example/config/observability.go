package config

import (
    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v4"
    "github.com/precision-soft/melody/v4/exception"
    tracenoop "go.opentelemetry.io/otel/trace/noop"
)

/* buildObservability builds one prometheus meter and hands it to both instrument families: the metrics middleware times the requests the pipeline handles (http.server.request.duration, in seconds), and the lifecycle decorator, wrapped around the whole kernel handler, times every request (melody.http.server.lifecycle.duration) — the security refusals and the other short-circuits the middleware never sees included. The decorator is wired for its metrics: its tracer is a no-op, because the spans are the tracing middleware's, which the otlp module installs with its own provider, and a lifecycle span beside it would put a second span on every trace. The no-op span keeps the extracted trace context, so the middleware's span stays under the caller's trace. */
func (instance *Module) buildObservability() {
    prometheusMetrics, metricsErr := melodyopentelemetry.NewPrometheusMetrics(melodyopentelemetry.PrometheusConfig{MeterName: "melody.example"})
    if nil != metricsErr {
        exception.Panic(exception.FromError(metricsErr))
    }

    meter := prometheusMetrics.Meter()

    middleware, middlewareErr := melodyopentelemetry.NewMetricsMiddleware(meter, melodyopentelemetry.MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        exception.Panic(exception.FromError(middlewareErr))
    }

    decorator, decoratorErr := melodyopentelemetry.NewHandlerDecorator(tracenoop.NewTracerProvider().Tracer("melody.example"), melodyopentelemetry.HandlerDecoratorConfig{
        Meter: meter,
    })
    if nil != decoratorErr {
        exception.Panic(exception.FromError(decoratorErr))
    }

    instance.metricsMiddleware = middleware
    instance.prometheusMetrics = prometheusMetrics
    instance.lifecycleDecorator = decorator
}

/* buildMetricsAuth reads the credential the metrics scraper presents. The exposition names every route and its traffic, so with a token set /metrics answers only a caller presenting it; an empty value leaves the door public, the development default of a stack without a scraper. */
func (instance *Module) buildMetricsAuth() {
    instance.metricsToken = instance.parameterValue(parameterMetricsToken)
}
