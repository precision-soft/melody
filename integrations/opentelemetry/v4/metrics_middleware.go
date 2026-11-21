package opentelemetry

import (
    nethttp "net/http"
    "reflect"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* DefaultDurationBoundaries answers a fresh copy of the bucket boundaries, in seconds, the HTTP semantic conventions advise for http.server.request.duration, so no caller can change the default another one reads. */
func DefaultDurationBoundaries() []float64 {
    return []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
}

/* MetricsMiddlewareConfig configures NewMetricsMiddleware; DurationBoundaries are the duration histogram's bucket boundaries in seconds, DefaultDurationBoundaries() when nil; an empty list is refused, since it would leave the histogram without buckets. */
type MetricsMiddlewareConfig struct {
    DurationBoundaries []float64
}

/* NewMetricsMiddleware records every handled request on the http.server.request.duration histogram of the HTTP semantic conventions, in seconds, with the method, the route, the scheme and the integer status code; a nil meter is refused. */
func NewMetricsMiddleware(meter metric.Meter, config MetricsMiddlewareConfig) (httpcontract.Middleware, error) {
    if nil == meter {
        return nil, exception.NewError("metrics middleware meter is nil", nil, nil)
    }

    if nil != config.DurationBoundaries && 0 == len(config.DurationBoundaries) {
        return nil, exception.NewError("MetricsMiddlewareConfig.DurationBoundaries is empty: leave it nil for DefaultDurationBoundaries()", nil, nil)
    }

    requestDuration, histogramErr := newDurationHistogram(meter, "http.server.request.duration", "duration of handled http requests", config.DurationBoundaries)
    if nil != histogramErr {
        return nil, exception.NewError("could not create the duration histogram", nil, histogramErr)
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            startedAt := time.Now()

            /* the wrapped writer captures the status a handler commits directly, the nil-response streaming or proxy shape MetricsRouteHandler and the websocket bridge use, so the per-route instruments record what the handler wrote rather than a default 200 */
            recorder := &statusRecordingResponseWriter{ResponseWriter: writer, statusCode: nethttp.StatusOK}

            var response httpcontract.Response
            var handlerErr error
            completed := false

            defer func() {
                statusCode := nethttp.StatusInternalServerError
                if true == completed {
                    statusCode = completedStatusCode(handlerErr, response, recorder)
                }

                attributes := metric.WithAttributes(
                    attribute.String("http.request.method", normalizedMethod(request.HttpRequest().Method)),
                    attribute.String("http.route", routeLabel(request)),
                    attribute.String("url.scheme", requestScheme(request)),
                    attribute.Int("http.response.status_code", statusCode),
                )

                requestDuration.Record(runtimeInstance.Context(), time.Since(startedAt).Seconds(), attributes)
            }()

            handlerResponse, nextErr := next(runtimeInstance, recorder, request)
            response = handlerResponse
            handlerErr = nextErr
            completed = true

            return handlerResponse, nextErr
        }
    }, nil
}

func newDurationHistogram(meter metric.Meter, name string, description string, boundaries []float64) (metric.Float64Histogram, error) {
    if nil == boundaries {
        boundaries = DefaultDurationBoundaries()
    }

    return meter.Float64Histogram(
        name,
        metric.WithDescription(description),
        metric.WithUnit("s"),
        metric.WithExplicitBucketBoundaries(boundaries...),
    )
}

/* requestScheme answers the scheme the kernel resolved for the request under its forwarded-headers policy, so a trusted proxy that terminated TLS reads as https here as in the access log; a request carrying no resolved scheme falls back to the connection's own. */
func requestScheme(request httpcontract.Request) string {
    if schemeValue, exists := request.Attributes().Get(melodyhttp.RequestAttributeScheme); true == exists {
        if scheme, isString := schemeValue.(string); true == isString && "" != scheme {
            return scheme
        }
    }

    return urlScheme(request.HttpRequest())
}

/* urlScheme answers https for a request that arrived over TLS on this connection and http otherwise: the lifecycle seam runs before the kernel resolves a scheme, so it reads the connection alone. */
func urlScheme(httpRequest *nethttp.Request) string {
    if nil != httpRequest.TLS {
        return "https"
    }

    return "http"
}

/* completedStatusCode answers the status of a handler that returned: a status the handler already committed to the writer is the one the connection carries, whatever it returned afterwards; otherwise the handler's error decides first and the response it returned next, in the order the kernel answers them, and a handler that returned neither is read off the writer */
func completedStatusCode(handlerErr error, response httpcontract.Response, recorder *statusRecordingResponseWriter) int {
    if true == recorder.wroteHeader {
        return recorder.observedStatusCode()
    }

    if nil != handlerErr {
        return statusCodeForError(handlerErr)
    }

    if false == isNilResponse(response) {
        return response.StatusCode()
    }

    return recorder.observedStatusCode()
}

/* statusCodeForError maps a handler error to the status the client receives, as the kernel does: an error answered as an infrastructure failure is a 503, an HttpException is sent at its own status, a 502 as a 502, a body past its limit as 413, and anything else is a server error. */
func statusCodeForError(handlerErr error) int {
    if true == exception.AnswersUnavailable(handlerErr) {
        return nethttp.StatusServiceUnavailable
    }

    httpException := exception.AsHttpException(handlerErr)
    if nil != httpException && 100 <= httpException.StatusCode() && 599 >= httpException.StatusCode() {
        return httpException.StatusCode()
    }

    var maxBytesError *nethttp.MaxBytesError
    if true == exception.ChainAs(handlerErr, &maxBytesError) {
        return nethttp.StatusRequestEntityTooLarge
    }

    return nethttp.StatusInternalServerError
}

/* isNilResponse answers true for a nil interface and for a typed-nil concrete response, since `nil != response` alone lets `var resp *SomeResponse; return resp, nil` through and the StatusCode() dereference would panic, charging the handler's defect to the observability layer. */
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
