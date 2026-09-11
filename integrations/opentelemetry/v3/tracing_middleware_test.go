package opentelemetry

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "io"
    "time"
    "go.opentelemetry.io/otel/codes"
    melodyhttp "github.com/precision-soft/melody/v3/http"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestTracingMiddleware_RequiresTracer(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a nil tracer to panic at construction")
        }
    }()

    NewTracingMiddleware(nil, nil)
}

func TestTracingMiddleware_RecordsServerSpan(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    tracer := provider.Tracer("melody-test")

    middleware := NewTracingMiddleware(tracer, nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(okHandler())

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    if false == strings.Contains(spans[0].Name(), nethttp.MethodGet) {
        t.Fatalf("unexpected span name: %s", spans[0].Name())
    }

    methodFound := false
    statusFound := false
    for _, attribute := range spans[0].Attributes() {
        if "http.request.method" == string(attribute.Key) && nethttp.MethodGet == attribute.Value.AsString() {
            methodFound = true
        }
        if "http.response.status_code" == string(attribute.Key) && 200 == int(attribute.Value.AsInt64()) {
            statusFound = true
        }
    }

    if false == methodFound || false == statusFound {
        t.Fatalf("expected method and status attributes on the span")
    }
}

func TestTracingMiddleware_ATypedNilResponseIsReadAsAbsentInsteadOfPanicking(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

    middleware := NewTracingMiddleware(provider.Tracer("melody-typed-nil-test"), nil)

    typedNilHandler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var response *typedNilProneResponse

        return response, nil
    }

    request, runtimeInstance := testRequestAndRuntime()

    if _, handlerErr := middleware(typedNilHandler)(runtimeInstance, httptest.NewRecorder(), request); nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected the span to end cleanly over a typed-nil response, got %d spans", len(spans))
    }
}

func TestTracingMiddleware_RecordsTheClientStatusWhenTheHandlerReturnsAnHttpExceptionError(t *testing.T) {
    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    tracer := provider.Tracer("melody-error-test")

    middleware := NewTracingMiddleware(tracer, nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, exception.NewHttpException(nethttp.StatusNotFound, "not found")
    })

    if _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatalf("expected the error to propagate")
    }

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    statusCode := int64(-1)
    for _, attribute := range spans[0].Attributes() {
        if "http.response.status_code" == string(attribute.Key) {
            statusCode = attribute.Value.AsInt64()
        }
    }

    if 404 != statusCode {
        t.Fatalf("expected the span to carry the client-facing 404 rather than omitting the status on error, got %d", statusCode)
    }
}

func TestTracingMiddleware_RecordsDirectWriteStatus(t *testing.T) {
    for _, scenario := range []string{"server error", "implicit write", "flush", "informational then final", "returned response after commit", "reader from", "hijack"} {
        t.Run(scenario, func(t *testing.T) {
            recorder := tracetest.NewSpanRecorder()
            provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
            request, runtimeInstance := testRequestAndRuntime()
            expectedStatus := int64(200)
            underlying := &streamingTestWriter{ResponseRecorder: httptest.NewRecorder()}
            handler := NewTracingMiddleware(provider.Tracer("direct-write"), nil)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                if err := nethttp.NewResponseController(writer).SetWriteDeadline(time.Now()); nil != err {
                    t.Fatal(err)
                }
                if err := writer.(nethttp.Pusher).Push("/asset", nil); nil != err {
                    t.Fatal(err)
                }
                switch scenario {
                case "server error":
                    expectedStatus = 503
                    writer.WriteHeader(503)
                case "implicit write":
                    _, _ = writer.Write([]byte("payload"))
                case "flush":
                    if err := nethttp.NewResponseController(writer).Flush(); nil != err {
                        t.Fatal(err)
                    }
                case "informational then final":
                    expectedStatus = 503
                    writer.WriteHeader(103)
                    writer.WriteHeader(503)
                case "returned response after commit":
                    expectedStatus = 202
                    writer.WriteHeader(202)
                    return melodyhttp.TextResponse(500, "unused"), nil
                case "reader from":
                    if _, err := writer.(io.ReaderFrom).ReadFrom(strings.NewReader("payload")); nil != err {
                        t.Fatal(err)
                    }
                case "hijack":
                    expectedStatus = 101
                    if _, _, err := writer.(nethttp.Hijacker).Hijack(); nil != err {
                        t.Fatal(err)
                    }
                }
                return nil, nil
            })
            if _, err := handler(runtimeInstance, underlying, request); nil != err {
                t.Fatal(err)
            }
            if false == underlying.deadlineSet || false == underlying.pushed {
                t.Fatal("writer capabilities were not forwarded")
            }
            if "flush" == scenario && false == underlying.Flushed {
                t.Fatal("flush was not forwarded")
            }
            if "hijack" == scenario && false == underlying.hijacked {
                t.Fatal("hijack was not forwarded")
            }
            spans := recorder.Ended()
            if 1 != len(spans) {
                t.Fatalf("expected one span, got %d", len(spans))
            }
            if 500 <= expectedStatus && codes.Error != spans[0].Status().Code {
                t.Fatal("server error did not mark the span as error")
            }
            for _, attribute := range spans[0].Attributes() {
                if "http.response.status_code" == string(attribute.Key) && expectedStatus == attribute.Value.AsInt64() {
                    return
                }
            }
            t.Fatalf("missing direct-write status %d", expectedStatus)
        })
    }
}
