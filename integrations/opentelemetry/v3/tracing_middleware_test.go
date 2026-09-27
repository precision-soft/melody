package opentelemetry

import (
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "go.opentelemetry.io/otel/codes"
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

func tracedHandlerSpan(t *testing.T, handlerErr error) sdktrace.ReadOnlySpan {
    t.Helper()

    recorder := tracetest.NewSpanRecorder()
    provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
    middleware := NewTracingMiddleware(provider.Tracer("melody-status-test"), nil)

    request, runtimeInstance := testRequestAndRuntime()
    handler := middleware(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, handlerErr
    })

    _, _ = handler(runtimeInstance, httptest.NewRecorder(), request)

    spans := recorder.Ended()
    if 1 != len(spans) {
        t.Fatalf("expected exactly one span, got %d", len(spans))
    }

    return spans[0]
}

/* a handled sub-500 is the client's error: the server span keeps its status unset, and the error stays recorded as the span's exception event */
func TestTracingMiddleware_AHandledSubFiveHundredLeavesTheSpanStatusUnset(t *testing.T) {
    span := tracedHandlerSpan(t, exception.NewHttpException(nethttp.StatusNotFound, "not found"))

    if codes.Unset != span.Status().Code || 1 != len(span.Events()) {
        t.Fatalf("expected an unset status and the error recorded as one event, got %v with %d events", span.Status(), len(span.Events()))
    }
}

func TestTracingMiddleware_AServerErrorSetsTheSpanStatusToError(t *testing.T) {
    span := tracedHandlerSpan(t, errors.New("database is gone"))

    if codes.Error != span.Status().Code || "database is gone" != span.Status().Description {
        t.Fatalf("expected the error status carrying the message, got %v", span.Status())
    }
}

type tracingTypedNilError struct {
    message *string
}

func (instance *tracingTypedNilError) Error() string {
    return *instance.message
}

/* a typed nil answers Error() with a panic: the middleware reads the message through the exception package and records the span without panicking on the handler's value */
func TestTracingMiddleware_ATypedNilHandlerErrorIsRecordedWithoutPanicking(t *testing.T) {
    var typedNil *tracingTypedNilError

    span := tracedHandlerSpan(t, typedNil)

    if codes.Error != span.Status().Code || 1 != len(span.Events()) {
        t.Fatalf("expected the typed nil recorded as a server error, got %v with %d events", span.Status(), len(span.Events()))
    }
}

/* a handler that writes the response itself returns no response, so the span reads the status off the writer: the status the client received, with the deadline and hijack doors still reachable through the recording writer */
func TestTracingMiddleware_RecordsTheStatusAHandlerCommitsDirectly(t *testing.T) {
    for name, scenario := range map[string]struct {
        handle   func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error)
        expected int64
    }{
        "a server error written directly": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusServiceUnavailable)

                return nil, nil
            },
            expected: nethttp.StatusServiceUnavailable,
        },
        "an implicit status on the first write": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                _, _ = writer.Write([]byte("payload"))

                return nil, nil
            },
            expected: nethttp.StatusOK,
        },
        "a flush": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                if flushErr := nethttp.NewResponseController(writer).Flush(); nil != flushErr {
                    t.Fatalf("flush: %v", flushErr)
                }

                return nil, nil
            },
            expected: nethttp.StatusOK,
        },
        "an early hint before the final status": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusEarlyHints)
                writer.WriteHeader(nethttp.StatusServiceUnavailable)

                return nil, nil
            },
            expected: nethttp.StatusServiceUnavailable,
        },
        "a response returned after a committed status": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusAccepted)

                return melodyhttp.TextResponse(nethttp.StatusInternalServerError, "unused"), nil
            },
            expected: nethttp.StatusAccepted,
        },
        "an error returned after a committed status": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                writer.WriteHeader(nethttp.StatusAccepted)

                return nil, errors.New("failed after the commit")
            },
            expected: nethttp.StatusAccepted,
        },
        "a copy through ReadFrom": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                if _, copyErr := writer.(io.ReaderFrom).ReadFrom(strings.NewReader("payload")); nil != copyErr {
                    t.Fatalf("read from: %v", copyErr)
                }

                return nil, nil
            },
            expected: nethttp.StatusOK,
        },
        "an upgrade": {
            handle: func(t *testing.T, writer nethttp.ResponseWriter) (httpcontract.Response, error) {
                if _, _, hijackErr := writer.(nethttp.Hijacker).Hijack(); nil != hijackErr {
                    t.Fatalf("hijack: %v", hijackErr)
                }

                return nil, nil
            },
            expected: nethttp.StatusSwitchingProtocols,
        },
    } {
        t.Run(name, func(t *testing.T) {
            spanRecorder := tracetest.NewSpanRecorder()
            provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
            request, runtimeInstance := testRequestAndRuntime()
            underlying := &streamingTestWriter{ResponseRecorder: httptest.NewRecorder()}

            handler := NewTracingMiddleware(provider.Tracer("direct-write"), nil)(func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                if deadlineErr := nethttp.NewResponseController(writer).SetWriteDeadline(time.Now().Add(time.Minute)); nil != deadlineErr {
                    t.Fatalf("set write deadline: %v", deadlineErr)
                }

                return scenario.handle(t, writer)
            })

            _, _ = handler(runtimeInstance, underlying, request)

            if false == underlying.deadlineSet {
                t.Fatal("the write deadline did not reach the writer behind the recorder")
            }

            spans := spanRecorder.Ended()
            if 1 != len(spans) {
                t.Fatalf("expected one span, got %d", len(spans))
            }

            recorded := int64(-1)
            for _, spanAttribute := range spans[0].Attributes() {
                if "http.response.status_code" == string(spanAttribute.Key) {
                    recorded = spanAttribute.Value.AsInt64()
                }
            }

            if scenario.expected != recorded {
                t.Fatalf("expected the span to carry %d, got %d", scenario.expected, recorded)
            }

            if (nethttp.StatusInternalServerError <= scenario.expected) != (codes.Error == spans[0].Status().Code) {
                t.Fatalf("expected the span in error only for a server status, got %v for %d", spans[0].Status().Code, scenario.expected)
            }
        })
    }
}
