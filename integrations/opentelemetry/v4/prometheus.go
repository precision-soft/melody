package opentelemetry

import (
    "context"
    nethttp "net/http"
    "sync"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"

    "github.com/precision-soft/melody/v4/exception"
)

const defaultMeterName = "github.com/precision-soft/melody/integrations/opentelemetry/v4"

/* PrometheusConfig configures NewPrometheusMetrics; MeterName names the meter's instrumentation scope, the module's import path when left out. */
type PrometheusConfig struct {
    MeterName string
}

/* PrometheusMetrics is a meter whose instruments the Prometheus exposition Handler serves, with the close door of the meter provider behind it; ModuleConfig.Prometheus registers it so the container's teardown closes it. */
type PrometheusMetrics struct {
    meter    metric.Meter
    handler  nethttp.Handler
    registry *prometheus.Registry
    provider *sdkmetric.MeterProvider
    mutex    sync.Mutex
    closed   bool
}

func NewPrometheusMetrics(config PrometheusConfig) (*PrometheusMetrics, error) {
    meterName := config.MeterName
    if "" == meterName {
        meterName = defaultMeterName
    }

    registry := prometheus.NewRegistry()

    exporter, exporterErr := otelprometheus.New(otelprometheus.WithRegisterer(registry))
    if nil != exporterErr {
        return nil, exception.NewError("could not create the prometheus exporter", nil, exporterErr)
    }

    provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))

    return &PrometheusMetrics{
        meter:    provider.Meter(meterName),
        handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
        registry: registry,
        provider: provider,
    }, nil
}

func (instance *PrometheusMetrics) Meter() metric.Meter {
    return instance.meter
}

func (instance *PrometheusMetrics) Handler() nethttp.Handler {
    return instance.handler
}

/* CloseWithContext shuts the meter provider down under the close context; a second call answers nil, and a context already done is answered its error with the provider left open, since the sdk latches a shutdown driven under a spent context. */
func (instance *PrometheusMetrics) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return nil
    }

    if contextErr := closeContext.Err(); nil != contextErr {
        return exception.NewError("the prometheus meter provider was not shut down: its close context is already done", nil, contextErr)
    }

    if shutdownErr := instance.provider.Shutdown(closeContext); nil != shutdownErr {
        return exception.NewError("could not shut the prometheus meter provider down", nil, shutdownErr)
    }

    instance.closed = true

    return nil
}
