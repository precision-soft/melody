package opentelemetry

import (
    "bufio"
    "errors"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    "go.opentelemetry.io/otel/trace"
)

func newDecoratorTestTracer(t *testing.T) (trace.Tracer, *tracetest.SpanRecorder) {
    t.Helper()

    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

    return provider.Tracer("melody-test"), recorder
}

func TestHandlerDecorator_TracesShortCircuitedRequest(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    /* the inner handler stands in for the kernel writing a 401 short-circuit before any middleware ran — the exact request shape the middleware seam never observes */
    denied := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        writer.WriteHeader(nethttp.StatusUnauthorized)
        _, _ = writer.Write([]byte(`{"error":"unauthorized"}`))
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/secure/me", nil)
    responseRecorder := httptest.NewRecorder()

    decorator(denied).ServeHTTP(responseRecorder, request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one lifecycle span for the denied request, got %d", len(spans))
    }

    statusFound := false
    for _, spanAttribute := range spans[0].Attributes() {
        if "http.response.status_code" == string(spanAttribute.Key) && nethttp.StatusUnauthorized == int(spanAttribute.Value.AsInt64()) {
            statusFound = true
        }
    }

    if false == statusFound {
        t.Fatalf("expected the lifecycle span to carry the 401 status attribute")
    }
}

func TestHandlerDecorator_InnerSpanParentsToLifecycleSpan(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    /* the inner handler starts a child span from the request context, standing in for the routed tracing middleware */
    routed := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        _, span := tracer.Start(request.Context(), "GET /hello")
        span.End()

        writer.WriteHeader(nethttp.StatusOK)
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)

    decorator(routed).ServeHTTP(httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 2 != len(spans) {
        t.Fatalf("expected the lifecycle span plus the routed child span, got %d", len(spans))
    }

    /* Ended() lists the child first (it ends first); the child must parent to the lifecycle span */
    child := spans[0]
    lifecycle := spans[1]

    if child.Parent().SpanID() != lifecycle.SpanContext().SpanID() {
        t.Fatalf("expected the routed span to be a child of the lifecycle span")
    }
}

func TestHandlerDecorator_MarksServerErrorStatus(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    failing := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        writer.WriteHeader(nethttp.StatusInternalServerError)
    })

    request := httptest.NewRequest(nethttp.MethodGet, "/boom", nil)

    decorator(failing).ServeHTTP(httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    if "Error" != spans[0].Status().Code.String() {
        t.Fatalf("expected the 500 to mark the span status Error, got %s", spans[0].Status().Code.String())
    }
}

func TestHandlerDecorator_RequiresTracer(t *testing.T) {
    if _, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{}); nil == decoratorErr {
        t.Fatalf("expected a nil tracer to be rejected")
    }
}

/* a middleware wrapper between the recorder and the connection that forwards Unwrap alone */
type unwrapOnlyResponseWriter struct {
    nethttp.ResponseWriter
}

func (instance *unwrapOnlyResponseWriter) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

type hijackableResponseWriter struct {
    *httptest.ResponseRecorder
    hijacked bool
}

func (instance *hijackableResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    instance.hijacked = true

    return nil, nil, nil
}

func TestStatusRecordingResponseWriter_FlushReachesThroughAWrapperThatForwardsUnwrapAlone(t *testing.T) {
    inner := httptest.NewRecorder()
    recorder := &statusRecordingResponseWriter{ResponseWriter: &unwrapOnlyResponseWriter{ResponseWriter: inner}, statusCode: nethttp.StatusOK}

    recorder.Flush()

    if false == inner.Flushed {
        t.Fatalf("expected the flush to reach the writer behind the wrapper")
    }
}

/* the error the server's own writer answers once the client has gone: the header is already committed, and the flush reports the write that failed */
type failingFlushResponseWriter struct {
    *httptest.ResponseRecorder
}

func (instance *failingFlushResponseWriter) FlushError() error {
    return errors.New("write: broken pipe")
}

func TestStatusRecordingResponseWriter_AFailedFlushKeepsTheStatusTheConnectionCarried(t *testing.T) {
    recorder := &statusRecordingResponseWriter{ResponseWriter: &failingFlushResponseWriter{ResponseRecorder: httptest.NewRecorder()}, statusCode: nethttp.StatusOK}

    recorder.Flush()
    recorder.WriteHeader(nethttp.StatusInternalServerError)

    if nethttp.StatusOK != recorder.statusCode {
        t.Fatalf("expected the status committed by the failed flush, got %d", recorder.statusCode)
    }
}

/* a writer with no flusher behind it is a writer the flush committed nothing on */
type plainResponseWriter struct {
    header nethttp.Header
}

func (instance *plainResponseWriter) Header() nethttp.Header {
    return instance.header
}

func (instance *plainResponseWriter) Write(payload []byte) (int, error) {
    return len(payload), nil
}

func (instance *plainResponseWriter) WriteHeader(statusCode int) {
}

func TestStatusRecordingResponseWriter_AFlushWithNoFlusherLeavesTheHeaderUnwritten(t *testing.T) {
    recorder := &statusRecordingResponseWriter{ResponseWriter: &plainResponseWriter{header: nethttp.Header{}}, statusCode: nethttp.StatusOK}

    recorder.Flush()
    recorder.WriteHeader(nethttp.StatusNotFound)

    if nethttp.StatusNotFound != recorder.statusCode {
        t.Fatalf("expected the status the handler wrote after a flush that committed nothing, got %d", recorder.statusCode)
    }
}

func TestStatusRecordingResponseWriter_HijackReachesThroughAWrapperThatForwardsUnwrapAlone(t *testing.T) {
    inner := &hijackableResponseWriter{ResponseRecorder: httptest.NewRecorder()}
    recorder := &statusRecordingResponseWriter{ResponseWriter: &unwrapOnlyResponseWriter{ResponseWriter: inner}, statusCode: nethttp.StatusOK}

    if _, _, hijackErr := recorder.Hijack(); nil != hijackErr || false == inner.hijacked || false == recorder.hijacked {
        t.Fatalf("expected the hijack to reach the writer behind the wrapper, got %v", hijackErr)
    }
}

func TestStatusRecordingResponseWriter_AnUpgradeIsObservedAsSwitchingProtocolsWhateverWasWritten(t *testing.T) {
    if statusCode := (&statusRecordingResponseWriter{statusCode: nethttp.StatusInternalServerError, hijacked: true}).observedStatusCode(); nethttp.StatusSwitchingProtocols != statusCode {
        t.Fatalf("expected %d for a hijacked connection, got %d", nethttp.StatusSwitchingProtocols, statusCode)
    }

    if statusCode := (&statusRecordingResponseWriter{statusCode: nethttp.StatusInternalServerError}).observedStatusCode(); nethttp.StatusInternalServerError != statusCode {
        t.Fatalf("expected the written %d, got %d", nethttp.StatusInternalServerError, statusCode)
    }
}

func TestHandlerDecorator_AnUpgradedConnectionIsTracedAsSwitchingProtocols(t *testing.T) {
    tracer, recorder := newDecoratorTestTracer(t)

    decorator, decoratorErr := NewHandlerDecorator(HandlerDecoratorConfig{Tracer: tracer})
    if nil != decoratorErr {
        t.Fatalf("unexpected decorator error: %v", decoratorErr)
    }

    upgrading := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        if _, _, hijackErr := nethttp.NewResponseController(writer).Hijack(); nil != hijackErr {
            t.Errorf("unexpected hijack error: %v", hijackErr)
        }
    })

    decorator(upgrading).ServeHTTP(&hijackableResponseWriter{ResponseRecorder: httptest.NewRecorder()}, httptest.NewRequest(nethttp.MethodGet, "/socket", nil))

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one lifecycle span, got %d", len(spans))
    }

    for _, spanAttribute := range spans[0].Attributes() {
        if "http.response.status_code" == string(spanAttribute.Key) {
            if nethttp.StatusSwitchingProtocols != int(spanAttribute.Value.AsInt64()) {
                t.Fatalf("expected the upgraded connection traced as %d, got %d", nethttp.StatusSwitchingProtocols, spanAttribute.Value.AsInt64())
            }

            return
        }
    }

    t.Fatal("expected the lifecycle span to carry a status attribute")
}

func TestStatusRecordingResponseWriter_AnEarlyHintIsNotRecordedAsTheFinalStatus(t *testing.T) {
    recorder := &statusRecordingResponseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: nethttp.StatusOK}

    recorder.WriteHeader(nethttp.StatusEarlyHints)
    recorder.WriteHeader(nethttp.StatusServiceUnavailable)

    if nethttp.StatusServiceUnavailable != recorder.observedStatusCode() {
        t.Fatalf("expected the final %d after an early hint, got %d", nethttp.StatusServiceUnavailable, recorder.observedStatusCode())
    }
}

func TestStatusRecordingResponseWriter_AWriteAfterALoneEarlyHintRecordsTheImplicitOk(t *testing.T) {
    recorder := &statusRecordingResponseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: nethttp.StatusOK}

    recorder.WriteHeader(nethttp.StatusEarlyHints)
    if _, writeErr := recorder.Write([]byte("body")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    if nethttp.StatusOK != recorder.observedStatusCode() || false == recorder.wroteHeader {
        t.Fatalf("expected the implicit %d committed by the write, got %d (committed %v)", nethttp.StatusOK, recorder.observedStatusCode(), recorder.wroteHeader)
    }
}

func TestStatusRecordingResponseWriter_SwitchingProtocolsIsRecordedAsTheFinalStatus(t *testing.T) {
    recorder := &statusRecordingResponseWriter{ResponseWriter: httptest.NewRecorder(), statusCode: nethttp.StatusOK}

    recorder.WriteHeader(nethttp.StatusSwitchingProtocols)
    recorder.WriteHeader(nethttp.StatusInternalServerError)

    if nethttp.StatusSwitchingProtocols != recorder.observedStatusCode() {
        t.Fatalf("expected %d to be recorded, got %d", nethttp.StatusSwitchingProtocols, recorder.observedStatusCode())
    }
}
