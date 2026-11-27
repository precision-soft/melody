package config

import (
    "strings"

    melodyopentelemetry "github.com/precision-soft/melody/integrations/opentelemetry/v4"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
    tracenoop "go.opentelemetry.io/otel/trace/noop"
)

/* MetricsPublicBootWarningName is the boot warning a deployment outside development writes when APP_METRICS_TOKEN is empty and /metrics is public */
const MetricsPublicBootWarningName = "example.metrics.public"

const (
    /* developmentMetricsToken is the token .env commits; it is only refused outside development, never a fallback */
    developmentMetricsToken = "melody-example-metrics-scrape-token-0001"

    metricsPublicBootWarningMessage = "/metrics is public outside development: APP_METRICS_TOKEN is empty, so any caller reaching the server reads every route and its traffic"
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

/* buildMetricsAuth reads the credential the metrics scraper presents. The exposition names every route and its traffic, so with a token set /metrics answers only a caller presenting it; an empty value leaves the door public in every environment, outside development with a boot warning (see RegisterBootWarnings). Outside development the committed token is public and refuses the boot. */
func (instance *Module) buildMetricsAuth() {
    token := strings.TrimSpace(instance.parameterValue(parameterMetricsToken))

    if "" != token && false == instance.isDevelopment() {
        refuseDevelopmentCredential(environmentKeyMetricsToken, token, []string{developmentMetricsToken}, nil)
    }

    instance.metricsToken = token
}

/* the code is declared in every environment and process, so a command's MELODY_CLI_BOOT_WARNINGS_IGNORED may name it; a cli process shares the deployment's .env and writes it too */
func (instance *Module) RegisterBootWarnings(registrar melodyapplicationcontract.BootWarningRegistrar) {
    registrar.DeclareBootWarning(MetricsPublicBootWarningName, melodyapplicationcontract.BootWarningConfig{WrittenInCli: true})

    if "" != instance.metricsToken || true == instance.isDevelopment() {
        return
    }

    registrar.AddBootWarning(melodyapplicationcontract.BootWarning{
        Name:    MetricsPublicBootWarningName,
        Message: metricsPublicBootWarningMessage,
        Context: map[string]any{"environmentKey": environmentKeyMetricsToken, "path": metricsPath},
    })
}

var _ melodyapplicationcontract.BootWarningModule = (*Module)(nil)
