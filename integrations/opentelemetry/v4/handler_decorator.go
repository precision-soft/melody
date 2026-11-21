package opentelemetry

import (
    "bufio"
    "io"
    "net"
    nethttp "net/http"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/trace"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* HandlerDecoratorConfig configures NewHandlerDecorator; Propagator is W3C TraceContext when left out. */
type HandlerDecoratorConfig struct {
    Propagator propagation.TextMapPropagator

    /* Meter is optional: when set, every request, short-circuited ones included, is timed on the melody.http.server.lifecycle.duration histogram, in seconds, distinct from the middleware's handled-request histogram so routed requests are not double-counted. */
    Meter metric.Meter

    /* DurationBoundaries are the lifecycle histogram's bucket boundaries in seconds, DefaultDurationBoundaries() when nil; an empty list, or a list set without Meter, is refused. */
    DurationBoundaries []float64
}

/* NewHandlerDecorator builds the outermost observability seam: it wraps the full nethttp.Handler the http kernel produces (register it through Application.RegisterHttpHandlerDecorator or the module), so requests the middlewares never see, security denials and other kernel.request short-circuits, listener-written responses and the panic-recovery path, still produce a span, and a metric when HandlerDecoratorConfig.Meter is set; a nil tracer is refused. The tracing middleware's span becomes a child of the lifecycle span through the context this decorator injects. */
func NewHandlerDecorator(tracer trace.Tracer, config HandlerDecoratorConfig) (applicationcontract.HttpHandlerDecorator, error) {
    if nil == tracer {
        return nil, exception.NewError("handler decorator tracer is nil", nil, nil)
    }

    if nil == config.Meter && nil != config.DurationBoundaries {
        return nil, exception.NewError("HandlerDecoratorConfig.DurationBoundaries is set without HandlerDecoratorConfig.Meter: the lifecycle histogram is built only with a meter", nil, nil)
    }

    if nil != config.DurationBoundaries && 0 == len(config.DurationBoundaries) {
        return nil, exception.NewError("HandlerDecoratorConfig.DurationBoundaries is empty: leave it nil for DefaultDurationBoundaries()", nil, nil)
    }

    propagator := config.Propagator
    if nil == propagator {
        propagator = propagation.TraceContext{}
    }

    var requestDuration metric.Float64Histogram

    if nil != config.Meter {
        histogram, histogramErr := newDurationHistogram(config.Meter, "melody.http.server.lifecycle.duration", "duration of http requests observed at the lifecycle seam, including short-circuited ones", config.DurationBoundaries)
        if nil != histogramErr {
            return nil, exception.NewError("could not create the lifecycle duration histogram", nil, histogramErr)
        }

        requestDuration = histogram
    }

    return func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            parentContext := propagator.Extract(request.Context(), propagation.HeaderCarrier(request.Header))

            /* the route is not resolved yet at this seam, so the span is named by the method alone, the OTel convention for an unmatched route, and the path travels as an attribute to keep cardinality bounded */
            spanContext, span := tracer.Start(
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

                if nil != requestDuration {
                    attributes := metric.WithAttributes(
                        attribute.String("http.request.method", normalizedMethod(request.Method)),
                        attribute.String("url.scheme", urlScheme(request)),
                        attribute.Int("http.response.status_code", statusCode),
                    )

                    requestDuration.Record(parentContext, time.Since(startedAt).Seconds(), attributes)
                }

                if nil != recoveredValue {
                    panic(recoveredValue)
                }
            }()

            next.ServeHTTP(recorder, request.WithContext(spanContext))
        })
    }, nil
}

/* statusRecordingResponseWriter captures the committed status code while forwarding the streaming and upgrade capabilities, as the http kernel's recording writer does: the kernel asserts its raw writer for Hijacker, and a flush that reached the connection through Unwrap alone would leave the commit unrecorded, so the wrapper carries its own Flush and Hijack. */
type statusRecordingResponseWriter struct {
    nethttp.ResponseWriter
    statusCode  int
    wroteHeader bool
    hijacked    bool
}

/* WriteHeader records the first final status: an informational 1xx header other than 101 (103 Early Hints) is forwarded without being recorded, since net/http sends it ahead of the final header the handler still writes. */
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

/* Flush goes through http.ResponseController, which follows Unwrap down the chain, so it reaches a flusher behind a middleware wrapper that forwards Unwrap alone. A flush that reached a flusher commits the header even when it fails, so only a writer with no flusher leaves the header unwritten, and a status set after a failed flush is not the one the connection carried. */
func (instance *statusRecordingResponseWriter) Flush() {
    flushErr := nethttp.NewResponseController(instance.ResponseWriter).Flush()
    if false == exception.ChainIs(flushErr, nethttp.ErrNotSupported) {
        instance.wroteHeader = true
    }
}

func (instance *statusRecordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    /* through http.ResponseController for the reason Flush gives: a wrapper between this one and the connection that forwards Unwrap alone still reaches the hijacker behind it */
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

/* ReadFrom is forwarded so the wrapper keeps satisfying io.ReaderFrom, preserving the underlying writer's sendfile fast path for file responses. */
func (instance *statusRecordingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
    instance.wroteHeader = true

    return io.Copy(instance.ResponseWriter, reader)
}

/* observedStatusCode is the status the connection carried as far as the writer saw it. A hijacked connection left the request/response model at the upgrade, so it is not recorded as the default 200, which would put an hours-long connection in the request duration series. */
func (instance *statusRecordingResponseWriter) observedStatusCode() int {
    if true == instance.hijacked {
        return nethttp.StatusSwitchingProtocols
    }

    return instance.statusCode
}

/* Unwrap exposes the underlying writer so http.ResponseController can reach its flush/hijack/deadline support through the wrapper, mirroring the http kernel's recording writer. */
func (instance *statusRecordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ nethttp.Flusher = (*statusRecordingResponseWriter)(nil)
var _ nethttp.Hijacker = (*statusRecordingResponseWriter)(nil)
var _ io.ReaderFrom = (*statusRecordingResponseWriter)(nil)
