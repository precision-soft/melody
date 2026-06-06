package opentelemetry

import (
    nethttp "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/metric"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func NewPrometheusMeter(meterName string) (metric.Meter, *prometheus.Registry, error) {
    registry := prometheus.NewRegistry()

    exporter, exporterErr := otelprometheus.New(otelprometheus.WithRegisterer(registry))
    if nil != exporterErr {
        return nil, nil, exception.NewError("could not create the prometheus exporter", nil, exporterErr)
    }

    provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))

    return provider.Meter(meterName), registry, nil
}

func MetricsHandler(registry *prometheus.Registry) nethttp.Handler {
    return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func NewMetricsMiddlewareWithPrometheus(meterName string) (httpcontract.Middleware, nethttp.Handler, error) {
    meter, registry, meterErr := NewPrometheusMeter(meterName)
    if nil != meterErr {
        return nil, nil, meterErr
    }

    middleware, middlewareErr := NewMetricsMiddleware(meter)
    if nil != middlewareErr {
        return nil, nil, middlewareErr
    }

    return middleware, MetricsHandler(registry), nil
}
