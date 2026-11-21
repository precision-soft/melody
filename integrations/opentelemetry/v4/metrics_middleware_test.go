package opentelemetry

import (
    "crypto/tls"
    "errors"
    "fmt"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strings"
    "testing"
    "time"

    "github.com/prometheus/client_golang/prometheus"
    "go.opentelemetry.io/otel/attribute"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func TestNormalizedMethod(t *testing.T) {
    standard := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE"}
    for _, method := range standard {
        if normalized := normalizedMethod(method); method != normalized {
            t.Fatalf("expected standard method %q to be preserved, got %q", method, normalized)
        }
    }

    for _, method := range []string{"BREW", "XYZZY", "M0001", "get", ""} {
        if normalized := normalizedMethod(method); "_OTHER" != normalized {
            t.Fatalf("expected non-standard method %q to normalize to _OTHER, got %q", method, normalized)
        }
    }
}

func TestMetricsMiddleware_RequiresMeter(t *testing.T) {
    if _, middlewareErr := NewMetricsMiddleware(nil, MetricsMiddlewareConfig{}); nil == middlewareErr {
        t.Fatalf("expected a nil meter to be rejected")
    }
}

func TestMetricsMiddleware_RecordsRequestMetrics(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-test"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(okHandler())

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    found := false
    for _, family := range families {
        if true == strings.Contains(family.GetName(), "http_server_request") {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected an http_server_request metric to be recorded")
    }
}

func TestMetricsMiddleware_RecordsServerErrorStatusWhenHandlerReturnsError(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-test-error"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, errors.New("boom")
    })

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatalf("expected the handler error to propagate")
    }

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    statusFound := ""
    for _, family := range families {
        if false == strings.Contains(family.GetName(), "http_server_request") {
            continue
        }
        for _, metricInstance := range family.GetMetric() {
            for _, label := range metricInstance.GetLabel() {
                if "http_response_status_code" == label.GetName() {
                    statusFound = label.GetValue()
                }
            }
        }
    }

    if "500" != statusFound {
        t.Fatalf("expected the status_code label to be 500 for an errored request, got %q", statusFound)
    }
}

func TestMetricsMiddleware_ReadsTheStatusADirectWriterCommitted(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-direct-writer-test"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    directWriter := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        writer.WriteHeader(nethttp.StatusServiceUnavailable)

        return nil, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(directWriter)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    if false == gatheredStatusLabels(t, registry)["503"] {
        t.Fatalf("expected the directly committed 503 to be recorded instead of the constructor's 200, got %v", gatheredStatusLabels(t, registry))
    }
}

func TestMetricsMiddleware_AnEarlyHintDoesNotHideTheFinalStatus(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-early-hint-test"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    directWriter := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        writer.WriteHeader(nethttp.StatusEarlyHints)
        writer.WriteHeader(nethttp.StatusServiceUnavailable)

        return nil, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(directWriter)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    labels := gatheredStatusLabels(t, registry)
    if false == labels["503"] || true == labels["103"] {
        t.Fatalf("expected the final 503 and no 103, got %v", labels)
    }
}

/* typedNilProneResponse exists so a test can hand the middleware the typed-nil shape a userland error branch produces. Its accessors DEREFERENCE the receiver, like every real response's do: a method body that ignores the receiver would run happily on a nil pointer, and the guard's mutant would survive against a fixture that cannot reproduce the panic it guards against. */
type typedNilProneResponse struct {
    statusCode int
    headers    nethttp.Header
    body       io.Reader
}

func (instance *typedNilProneResponse) StatusCode() int                   { return instance.statusCode }
func (instance *typedNilProneResponse) SetStatusCode(statusCode int)      { instance.statusCode = statusCode }
func (instance *typedNilProneResponse) Headers() nethttp.Header           { return instance.headers }
func (instance *typedNilProneResponse) SetHeaders(headers nethttp.Header) { instance.headers = headers }
func (instance *typedNilProneResponse) BodyReader() io.Reader             { return instance.body }
func (instance *typedNilProneResponse) SetBodyReader(reader io.Reader)    { instance.body = reader }

func TestMetricsMiddleware_ATypedNilResponseIsReadAsAbsentInsteadOfPanicking(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-typed-nil-test"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    typedNilHandler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var response *typedNilProneResponse

        return response, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(typedNilHandler)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    if false == gatheredStatusLabels(t, registry)["200"] {
        t.Fatalf("expected the typed-nil response to be read as absent and the committed 200 recorded, got %v", gatheredStatusLabels(t, registry))
    }
}

func gatheredStatusLabels(t *testing.T, registry *prometheus.Registry) map[string]bool {
    t.Helper()

    families, gatherErr := registry.Gather()
    if nil != gatherErr {
        t.Fatalf("gather: %v", gatherErr)
    }

    statuses := map[string]bool{}
    for _, family := range families {
        for _, metricInstance := range family.GetMetric() {
            for _, label := range metricInstance.GetLabel() {
                if "http_response_status_code" == label.GetName() {
                    statuses[label.GetValue()] = true
                }
            }
        }
    }

    return statuses
}

func TestMetricsMiddleware_RecordsTheClientStatusOfAnHttpExceptionError(t *testing.T) {
    prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-http-exception-test"})
    if nil != meterErr {
        t.Fatalf("meter: %v", meterErr)
    }
    meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, exception.NewHttpException(nethttp.StatusNotFound, "not found")
    })

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatalf("expected the handler error to propagate")
    }

    labels := gatheredStatusLabels(t, registry)
    if false == labels["404"] {
        t.Fatalf("expected a handled 404 to be recorded at its client-facing status, not folded into 5xx, got %v", labels)
    }
    if true == labels["500"] {
        t.Fatalf("expected the deliberate 404 not to be recorded as a server error, got %v", labels)
    }
}

func TestStatusCodeForError_MapsHttpExceptionsAtTheirOwnStatusABodyLimitTo413AndDefaultsToFiveHundred(t *testing.T) {
    if 404 != statusCodeForError(exception.NewHttpException(nethttp.StatusNotFound, "not found")) {
        t.Fatalf("expected a 404 http exception to map to 404")
    }
    if nethttp.StatusBadGateway != statusCodeForError(exception.NewHttpException(nethttp.StatusBadGateway, "upstream")) {
        t.Fatalf("expected a 502 http exception graphed at the 502 the kernel sends")
    }
    if nethttp.StatusRequestEntityTooLarge != statusCodeForError(fmt.Errorf("read body: %w", &nethttp.MaxBytesError{Limit: 16})) {
        t.Fatalf("expected a body past its limit graphed at the 413 the kernel sends")
    }
    if nethttp.StatusServiceUnavailable != statusCodeForError(exception.MarkInfrastructureFailure(errors.New("redis is down"))) {
        t.Fatal("expected a marked error labelled 503, the status the renderer answers")
    }

    if nethttp.StatusServiceUnavailable != statusCodeForError(exception.MarkInfrastructureFailure(exception.NewHttpException(nethttp.StatusNotFound, "no such row"))) {
        t.Fatal("expected a store's status beneath the mark labelled 503")
    }

    if nethttp.StatusBadRequest != statusCodeForError(exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "bad", Cause: exception.MarkInfrastructureFailure(errors.New("down"))})) {
        t.Fatal("expected an http status around the marked cause kept")
    }

    if nethttp.StatusInternalServerError != statusCodeForError(errors.New("plain")) {
        t.Fatalf("expected a non-http error to be a server error")
    }
}

func TestCompletedStatusCode_ACommittedStatusDecidesThenTheErrorThenTheResponseThenTheWriter(t *testing.T) {
    uncommittedRecorder := &statusRecordingResponseWriter{statusCode: nethttp.StatusAccepted}

    cases := map[string]struct {
        handlerErr error
        response   httpcontract.Response
        recorder   *statusRecordingResponseWriter
        expected   int
    }{
        "committed status over error and response": {
            handlerErr: exception.NewHttpException(nethttp.StatusNotFound, "not found"),
            response:   &typedNilProneResponse{statusCode: nethttp.StatusCreated},
            recorder:   &statusRecordingResponseWriter{statusCode: nethttp.StatusAccepted, wroteHeader: true},
            expected:   nethttp.StatusAccepted,
        },
        "upgrade over error and response": {
            handlerErr: exception.NewHttpException(nethttp.StatusNotFound, "not found"),
            response:   &typedNilProneResponse{statusCode: nethttp.StatusCreated},
            recorder:   &statusRecordingResponseWriter{statusCode: nethttp.StatusOK, wroteHeader: true, hijacked: true},
            expected:   nethttp.StatusSwitchingProtocols,
        },
        "error over response": {
            handlerErr: exception.NewHttpException(nethttp.StatusNotFound, "not found"),
            response:   &typedNilProneResponse{statusCode: nethttp.StatusCreated},
            recorder:   uncommittedRecorder,
            expected:   nethttp.StatusNotFound,
        },
        "response over the writer": {response: &typedNilProneResponse{statusCode: nethttp.StatusCreated}, recorder: uncommittedRecorder, expected: nethttp.StatusCreated},
        "typed-nil response":       {response: (*typedNilProneResponse)(nil), recorder: uncommittedRecorder, expected: nethttp.StatusAccepted},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            if statusCode := completedStatusCode(testCase.handlerErr, testCase.response, testCase.recorder); testCase.expected != statusCode {
                t.Fatalf("expected %d, got %d", testCase.expected, statusCode)
            }
        })
    }
}

/* a handler that committed a status and then returned something else has already answered the client: the committed status is the one graphed, over a returned response as over a returned error */
func TestMetricsMiddleware_ACommittedStatusWinsOverWhatTheHandlerReturnsAfterIt(t *testing.T) {
    for name, returned := range map[string]func() (httpcontract.Response, error){
        "a returned response": func() (httpcontract.Response, error) {
            return melodyhttp.TextResponse(nethttp.StatusInternalServerError, "unused"), nil
        },
        "a returned error": func() (httpcontract.Response, error) {
            return nil, errors.New("failed after the commit")
        },
    } {
        t.Run(name, func(t *testing.T) {
            prometheusMetrics, meterErr := NewPrometheusMetrics(PrometheusConfig{MeterName: "melody-committed-status-test"})
            if nil != meterErr {
                t.Fatalf("meter: %v", meterErr)
            }
            meter, registry := prometheusMetrics.Meter(), prometheusMetrics.registry

            middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
            if nil != middlewareErr {
                t.Fatalf("middleware: %v", middlewareErr)
            }

            committing := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusAccepted)

                return returned()
            }

            request, runtimeInstance := testRequestAndRuntime()

            _, _ = middleware(committing)(runtimeInstance, httptest.NewRecorder(), request)

            labels := gatheredStatusLabels(t, registry)
            if false == labels["202"] || true == labels["500"] {
                t.Fatalf("expected the committed 202 and no 500, got %v", labels)
            }
        })
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func TestStatusCodeForError_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if 500 != statusCodeForError(newCyclicChainError()) {
            t.Error("expected a cyclic chain to read as a server failure")
        }
    })
}

func TestMetricsMiddleware_RecordsTheConventionsDurationInSecondsWithAnIntegerStatus(t *testing.T) {
    reader := sdkmetric.NewManualReader()
    meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("conventions")

    middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    held := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        time.Sleep(20 * time.Millisecond)

        return nil, errors.New("handler failed")
    }

    _, _ = middleware(held)(runtimeInstance, httptest.NewRecorder(), request)

    instrument, point := collectHistogram(t, reader, "http.server.request.duration")
    if "s" != instrument.Unit {
        t.Fatalf("expected the unit s, got %q", instrument.Unit)
    }

    if false == reflect.DeepEqual(conventionDurationBoundaries, point.Bounds) {
        t.Fatalf("expected the conventions' boundaries, got %v", point.Bounds)
    }

    if 0.02 > point.Sum || 1 <= point.Sum {
        t.Fatalf("expected a sum in seconds for a 20ms handler, got %v", point.Sum)
    }

    status, hasStatus := point.Attributes.Value(attribute.Key("http.response.status_code"))
    if false == hasStatus || attribute.INT64 != status.Type() || 500 != status.AsInt64() {
        t.Fatalf("expected an integer status 500, got %v", status)
    }

    if scheme, hasScheme := point.Attributes.Value(attribute.Key("url.scheme")); false == hasScheme || "http" != scheme.AsString() {
        t.Fatalf("expected url.scheme http, got %v", scheme)
    }

    for _, name := range instrumentNames(t, reader) {
        if "http.server.request.count" == name {
            t.Fatal("expected no request counter beside the histogram")
        }
    }
}

/* behind a TLS-terminating trusted proxy the connection is plain while the kernel resolved https: the series follows the kernel, as the access log does, and a request with no resolved scheme reads its connection */
func TestMetricsMiddleware_UrlSchemeFollowsTheKernelsResolvedScheme(t *testing.T) {
    for _, scenario := range []struct {
        name           string
        resolvedScheme string
        overTls        bool
        expected       string
    }{
        {name: "resolved https over a plain connection", resolvedScheme: "https", overTls: false, expected: "https"},
        {name: "resolved http over a tls connection", resolvedScheme: "http", overTls: true, expected: "http"},
        {name: "no resolved scheme over a tls connection", resolvedScheme: "", overTls: true, expected: "https"},
        {name: "no resolved scheme over a plain connection", resolvedScheme: "", overTls: false, expected: "http"},
    } {
        t.Run(scenario.name, func(t *testing.T) {
            reader := sdkmetric.NewManualReader()
            meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("scheme")

            middleware, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{})
            if nil != middlewareErr {
                t.Fatalf("middleware: %v", middlewareErr)
            }

            request, runtimeInstance := testRequestAndRuntime()
            if true == scenario.overTls {
                request.HttpRequest().TLS = &tls.ConnectionState{}
            }
            if "" != scenario.resolvedScheme {
                request.Attributes().Set(melodyhttp.RequestAttributeScheme, scenario.resolvedScheme)
            }

            _, _ = middleware(okHandler())(runtimeInstance, httptest.NewRecorder(), request)

            _, point := collectHistogram(t, reader, "http.server.request.duration")
            if scheme, hasScheme := point.Attributes.Value(attribute.Key("url.scheme")); false == hasScheme || scenario.expected != scheme.AsString() {
                t.Fatalf("expected url.scheme %s, got %v", scenario.expected, scheme)
            }
        })
    }
}

func TestMetricsMiddleware_TheExpositionFamilyIsInSecondsAndSetBoundariesReplaceTheDefault(t *testing.T) {
    prometheusMetrics, metricsErr := NewPrometheusMetrics(PrometheusConfig{})
    if nil != metricsErr {
        t.Fatalf("metrics: %v", metricsErr)
    }

    middleware, middlewareErr := NewMetricsMiddleware(prometheusMetrics.Meter(), MetricsMiddlewareConfig{DurationBoundaries: []float64{0.5, 3}})
    if nil != middlewareErr {
        t.Fatalf("middleware: %v", middlewareErr)
    }

    request, runtimeInstance := testRequestAndRuntime()
    _, _ = middleware(okHandler())(runtimeInstance, httptest.NewRecorder(), request)

    recorder := httptest.NewRecorder()
    prometheusMetrics.Handler().ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/metrics", nil))
    body := recorder.Body.String()

    if false == strings.Contains(body, "http_server_request_duration_seconds_bucket") || false == strings.Contains(body, `le="3"`) || true == strings.Contains(body, `le="0.005"`) {
        t.Fatalf("expected the seconds family with the set boundaries only, got %s", body)
    }
}

func TestDefaultDurationBoundaries_AnswersAFreshCopyOfTheConventionsList(t *testing.T) {
    first := DefaultDurationBoundaries()
    if false == reflect.DeepEqual(conventionDurationBoundaries, first) {
        t.Fatalf("expected the conventions' boundaries, got %v", first)
    }

    first[0] = 42

    if second := DefaultDurationBoundaries(); false == reflect.DeepEqual(conventionDurationBoundaries, second) {
        t.Fatalf("expected a caller's edit to leave the default unchanged, got %v", second)
    }
}

/* an empty list would build a histogram with no buckets, a sum and a count only, so it is refused rather than read as left out */
func TestNewMetricsMiddleware_AnEmptyDurationBoundariesListIsRefused(t *testing.T) {
    meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("empty")

    _, middlewareErr := NewMetricsMiddleware(meter, MetricsMiddlewareConfig{DurationBoundaries: []float64{}})
    if nil == middlewareErr || false == strings.Contains(middlewareErr.Error(), "MetricsMiddlewareConfig.DurationBoundaries is empty") {
        t.Fatalf("expected an empty DurationBoundaries refused naming the field, got %v", middlewareErr)
    }
}
