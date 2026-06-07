package opentelemetry

import (
    nethttp "net/http"
    "strconv"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewMetricsMiddleware(meter metric.Meter) (httpcontract.Middleware, error) {
    requestCount, counterErr := meter.Int64Counter(
        "http.server.request.count",
        metric.WithDescription("number of handled http requests"),
    )
    if nil != counterErr {
        return nil, exception.NewError("could not create the request counter", nil, counterErr)
    }

    requestDuration, histogramErr := meter.Float64Histogram(
        "http.server.request.duration",
        metric.WithDescription("duration of handled http requests in milliseconds"),
        metric.WithUnit("ms"),
    )
    if nil != histogramErr {
        return nil, exception.NewError("could not create the duration histogram", nil, histogramErr)
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            startedAt := time.Now()

            var response httpcontract.Response
            var handlerErr error
            completed := false

            defer func() {
                statusCode := nethttp.StatusInternalServerError
                if true == completed {
                    statusCode = nethttp.StatusOK
                    if nil != response {
                        statusCode = response.StatusCode()
                    }
                    if nil != handlerErr {
                        statusCode = nethttp.StatusInternalServerError
                    }
                }

                attributes := metric.WithAttributes(
                    attribute.String("http.request.method", request.HttpRequest().Method),
                    attribute.String("http.route", routeLabel(request)),
                    attribute.String("http.response.status_code", strconv.Itoa(statusCode)),
                )

                requestCount.Add(runtimeInstance.Context(), 1, attributes)
                requestDuration.Record(runtimeInstance.Context(), float64(time.Since(startedAt).Microseconds())/1000.0, attributes)
            }()

            handlerResponse, nextErr := next(runtimeInstance, writer, request)
            response = handlerResponse
            handlerErr = nextErr
            completed = true

            return handlerResponse, nextErr
        }
    }, nil
}

func routeLabel(request httpcontract.Request) string {
    route := request.RoutePattern()
    if "" == route {
        return "unmatched"
    }

    return route
}
