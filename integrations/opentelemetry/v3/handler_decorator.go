package opentelemetry

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"
    "strconv"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/trace"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/exception"
)

type HandlerDecoratorConfig struct {
    Tracer trace.Tracer

    Propagator propagation.TextMapPropagator

    /* Meter is optional; its lifecycle instruments are distinct from the middleware's, so routed requests do not double-count. */
    Meter metric.Meter
}

/* NewHandlerDecorator wraps the full kernel handler, so requests the middlewares never see still produce a span. */
func NewHandlerDecorator(config HandlerDecoratorConfig) (applicationcontract.HttpHandlerDecorator, error) {
    if nil == config.Tracer {
        return nil, exception.NewError("handler decorator tracer is nil", nil, nil)
    }

    propagator := config.Propagator
    if nil == propagator {
        propagator = propagation.TraceContext{}
    }

    var requestCount metric.Int64Counter
    var requestDuration metric.Float64Histogram

    if nil != config.Meter {
        counter, counterErr := config.Meter.Int64Counter(
            "http.server.lifecycle.request.count",
            metric.WithDescription("number of http requests observed at the lifecycle seam, including short-circuited ones"),
        )
        if nil != counterErr {
            return nil, exception.NewError("could not create the lifecycle request counter", nil, counterErr)
        }

        histogram, histogramErr := config.Meter.Float64Histogram(
            "http.server.lifecycle.request.duration",
            metric.WithDescription("duration of http requests observed at the lifecycle seam in milliseconds"),
            metric.WithUnit("ms"),
        )
        if nil != histogramErr {
            return nil, exception.NewError("could not create the lifecycle duration histogram", nil, histogramErr)
        }

        requestCount = counter
        requestDuration = histogram
    }

    return func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            parentContext := propagator.Extract(request.Context(), propagation.HeaderCarrier(request.Header))

            /* the route is not resolved at this seam: the span is named by the method alone, to keep cardinality bounded */
            spanContext, span := config.Tracer.Start(
                parentContext,
                normalizedMethod(request.Method),
                trace.WithSpanKind(trace.SpanKindServer),
                trace.WithAttributes(
                    attribute.String("http.request.method", normalizedMethod(request.Method)),
                    attribute.String("url.path", request.URL.Path),
                ),
            )
            defer span.End()

            recorder := &statusRecordingResponseWriter{ResponseWriter: writer, statusCode: nethttp.StatusOK}
            startedAt := time.Now()

            defer func() {
                recoveredValue := recover()

                statusCode := recorder.observedStatusCode()

                if nil != recoveredValue {
                    statusCode = nethttp.StatusInternalServerError
                    span.SetStatus(codes.Error, "handler panicked")
                }

                span.SetAttributes(attribute.Int("http.response.status_code", statusCode))
                if nil == recoveredValue && nethttp.StatusInternalServerError <= statusCode {
                    span.SetStatus(codes.Error, nethttp.StatusText(statusCode))
                }

                if nil != requestCount {
                    attributes := metric.WithAttributes(
                        attribute.String("http.request.method", normalizedMethod(request.Method)),
                        attribute.String("http.response.status_code", strconv.Itoa(statusCode)),
                    )

                    requestCount.Add(parentContext, 1, attributes)
                    requestDuration.Record(parentContext, float64(time.Since(startedAt).Microseconds())/1000.0, attributes)
                }

                if nil != recoveredValue {
                    panic(recoveredValue)
                }
            }()

            next.ServeHTTP(recorder, request.WithContext(spanContext))
        })
    }, nil
}

/* the wrapper carries its own Flush and Hijack: the kernel asserts its raw writer for Hijacker */
type statusRecordingResponseWriter struct {
    nethttp.ResponseWriter
    statusCode  int
    wroteHeader bool
    hijacked    bool
}

/* WriteHeader forwards an informational 1xx other than 101 unrecorded: the final header is still to come. */
func (instance *statusRecordingResponseWriter) WriteHeader(statusCode int) {
    if false == instance.wroteHeader && (nethttp.StatusOK <= statusCode || nethttp.StatusSwitchingProtocols == statusCode) {
        instance.statusCode = statusCode
        instance.wroteHeader = true
    }

    instance.ResponseWriter.WriteHeader(statusCode)
}

func (instance *statusRecordingResponseWriter) Write(payload []byte) (int, error) {
    instance.wroteHeader = true

    return instance.ResponseWriter.Write(payload)
}

/* Flush goes through http.ResponseController, so it reaches a flusher behind a wrapper that forwards Unwrap alone. */
func (instance *statusRecordingResponseWriter) Flush() {
    flushErr := nethttp.NewResponseController(instance.ResponseWriter).Flush()
    if false == exception.ChainIs(flushErr, nethttp.ErrNotSupported) {
        instance.wroteHeader = true
    }
}

func (instance *statusRecordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    /* through http.ResponseController: a wrapper that forwards Unwrap alone still reaches the hijacker behind it */
    connection, readWriter, hijackErr := nethttp.NewResponseController(instance.ResponseWriter).Hijack()
    if true == exception.ChainIs(hijackErr, nethttp.ErrNotSupported) {
        return nil, nil, exception.NewError("the underlying response writer does not support hijacking", nil, nil)
    }
    if nil == hijackErr {
        instance.hijacked = true
        instance.wroteHeader = true
    }

    return connection, readWriter, hijackErr
}

/* ReadFrom keeps the wrapper an io.ReaderFrom, preserving the underlying writer's sendfile fast path. */
func (instance *statusRecordingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
    instance.wroteHeader = true

    return io.Copy(instance.ResponseWriter, reader)
}

/* a hijacked connection answers 101, not the default 200, so it stays out of the 200 duration series */
func (instance *statusRecordingResponseWriter) observedStatusCode() int {
    if true == instance.hijacked {
        return nethttp.StatusSwitchingProtocols
    }

    return instance.statusCode
}

func (instance *statusRecordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ nethttp.Flusher = (*statusRecordingResponseWriter)(nil)
var _ nethttp.Hijacker = (*statusRecordingResponseWriter)(nil)
var _ io.ReaderFrom = (*statusRecordingResponseWriter)(nil)
