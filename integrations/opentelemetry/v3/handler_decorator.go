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

    /* Meter is optional: when set, every request — including short-circuited ones — is counted and timed under the lifecycle instrument names (distinct from the middleware's handled-request instruments, so routed requests are not double-counted under one name). */
    Meter metric.Meter
}

/* NewHandlerDecorator builds the outermost observability seam: it wraps the full nethttp.Handler the http kernel produces (register it through Application.RegisterHttpHandlerDecorator or the module), so requests the middlewares never see — security denials and other kernel.request short-circuits, listener-written responses, the panic-recovery path — still produce a span and a metric. The tracing middleware keeps instrumenting the routed slice; its span becomes a child of the lifecycle span through the context this decorator injects, giving denied requests one span and routed requests a parent/child pair. */
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

            /* the route is not resolved yet at this seam, so the span name follows the OTel semantic convention for an unmatched route: the method alone; the path travels as an attribute instead of the name to keep cardinality bounded */
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

                statusCode := recorder.statusCode

                /* @important a hijacked connection left the request/response model at the upgrade, so recording it as the constructor's default 200 puts a connection that lives for hours in the same duration series as an ordinary request and destroys the latency distribution */
                if true == recorder.hijacked {
                    statusCode = nethttp.StatusSwitchingProtocols
                }

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

/* statusRecordingResponseWriter captures the committed status code while optimistically forwarding the streaming/upgrade capabilities, mirroring the http kernel's recording writer: the kernel probes its raw writer for Flusher/Hijacker, so this wrapper must keep satisfying them and delegate with a runtime probe of its own. */
type statusRecordingResponseWriter struct {
    nethttp.ResponseWriter
    statusCode  int
    wroteHeader bool
    hijacked    bool
}

func (instance *statusRecordingResponseWriter) WriteHeader(statusCode int) {
    if false == instance.wroteHeader {
        instance.statusCode = statusCode
        instance.wroteHeader = true
    }

    instance.ResponseWriter.WriteHeader(statusCode)
}

func (instance *statusRecordingResponseWriter) Write(payload []byte) (int, error) {
    instance.wroteHeader = true

    return instance.ResponseWriter.Write(payload)
}

func (instance *statusRecordingResponseWriter) Flush() {
    flusher, isFlusher := instance.ResponseWriter.(nethttp.Flusher)
    if true == isFlusher {
        instance.wroteHeader = true

        flusher.Flush()
    }
}

func (instance *statusRecordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    hijacker, isHijacker := instance.ResponseWriter.(nethttp.Hijacker)
    if false == isHijacker {
        return nil, nil, exception.NewError("the underlying response writer does not support hijacking", nil, nil)
    }

    connection, readWriter, hijackErr := hijacker.Hijack()
    if nil == hijackErr {
        instance.hijacked = true
        instance.wroteHeader = true
    }

    return connection, readWriter, hijackErr
}

/* @info ReadFrom is forwarded so the wrapper keeps satisfying io.ReaderFrom, preserving the underlying writer's sendfile fast path for file responses. */
func (instance *statusRecordingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
    instance.wroteHeader = true

    return io.Copy(instance.ResponseWriter, reader)
}

/* @info Unwrap exposes the underlying writer so http.ResponseController can reach its flush/hijack/deadline support through the wrapper, mirroring the http kernel's recording writer. */
func (instance *statusRecordingResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

var _ nethttp.Flusher = (*statusRecordingResponseWriter)(nil)
var _ nethttp.Hijacker = (*statusRecordingResponseWriter)(nil)
var _ io.ReaderFrom = (*statusRecordingResponseWriter)(nil)
