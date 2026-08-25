package opentelemetry

import (
    nethttp "net/http"
    "reflect"
    "strconv"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewMetricsMiddleware(meter metric.Meter) (httpcontract.Middleware, error) {
    /* fail fast on a nil meter at construction rather than nil-panicking on the first meter.Int64Counter call, matching NewHandlerDecorator which requires a non-nil Meter for its lifecycle instruments. */
    if nil == meter {
        return nil, exception.NewError("metrics middleware meter is nil", nil, nil)
    }

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

            /* the wrapped writer captures the status a handler commits DIRECTLY — the nil-response streaming/proxy shape, which this package's own MetricsRouteHandler and the websocket bridge both use. Without it that shape recorded the constructor's 200 whatever the handler wrote, so a route failing 100% of the time could graph as 100% success on the per-route instruments. */
            recorder := &statusRecordingResponseWriter{ResponseWriter: writer, statusCode: nethttp.StatusOK}

            var response httpcontract.Response
            var handlerErr error
            completed := false

            defer func() {
                statusCode := nethttp.StatusInternalServerError
                if true == completed {
                    statusCode = recorder.statusCode
                    if true == recorder.hijacked {
                        statusCode = nethttp.StatusSwitchingProtocols
                    }
                    if false == isNilResponse(response) {
                        statusCode = response.StatusCode()
                    }
                    if nil != handlerErr {
                        statusCode = nethttp.StatusInternalServerError
                    }
                }

                attributes := metric.WithAttributes(
                    attribute.String("http.request.method", normalizedMethod(request.HttpRequest().Method)),
                    attribute.String("http.route", routeLabel(request)),
                    attribute.String("http.response.status_code", strconv.Itoa(statusCode)),
                )

                requestCount.Add(runtimeInstance.Context(), 1, attributes)
                requestDuration.Record(runtimeInstance.Context(), float64(time.Since(startedAt).Microseconds())/1000.0, attributes)
            }()

            handlerResponse, nextErr := next(runtimeInstance, recorder, request)
            response = handlerResponse
            handlerErr = nextErr
            completed = true

            return handlerResponse, nextErr
        }
    }, nil
}

/* isNilResponse answers true for a nil interface AND for a typed-nil concrete response: `nil != response` alone lets a handler's `var resp *SomeResponse; return resp, nil` through, and the middleware's own StatusCode() dereference then panics — a panic charged to the observability layer while the defect sits in the handler. */
func isNilResponse(response httpcontract.Response) bool {
    if nil == response {
        return true
    }

    value := reflect.ValueOf(response)
    switch value.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
        return value.IsNil()
    }

    return false
}

func routeLabel(request httpcontract.Request) string {
    route := request.RoutePattern()
    if "" == route {
        return "unmatched"
    }

    return route
}

var standardHttpMethods = map[string]bool{
    nethttp.MethodGet:     true,
    nethttp.MethodHead:    true,
    nethttp.MethodPost:    true,
    nethttp.MethodPut:     true,
    nethttp.MethodPatch:   true,
    nethttp.MethodDelete:  true,
    nethttp.MethodConnect: true,
    nethttp.MethodOptions: true,
    nethttp.MethodTrace:   true,
}

func normalizedMethod(method string) string {
    if true == standardHttpMethods[method] {
        return method
    }

    return "_OTHER"
}
