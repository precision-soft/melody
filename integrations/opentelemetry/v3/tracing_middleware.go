package opentelemetry

import (
    "errors"
    nethttp "net/http"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/trace"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewTracingMiddleware(tracer trace.Tracer, propagator propagation.TextMapPropagator) httpcontract.Middleware {
    /* a nil tracer is refused at construction rather than panicking on the first request, as NewHandlerDecorator's nil-Tracer guard does; a no-error constructor cannot report it, so it panics with a clear cause like other constructors of required dependencies */
    if nil == tracer {
        exception.Panic(exception.NewError("tracing middleware tracer is nil", nil, nil))
    }

    if nil == propagator {
        propagator = propagation.TraceContext{}
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            httpRequest := request.HttpRequest()

            parentContext := propagator.Extract(runtimeInstance.Context(), propagation.HeaderCarrier(httpRequest.Header))

            spanContext, span := tracer.Start(
                parentContext,
                spanName(request),
                trace.WithSpanKind(trace.SpanKindServer),
                trace.WithAttributes(
                    attribute.String("http.request.method", normalizedMethod(httpRequest.Method)),
                    attribute.String("http.route", routeLabel(request)),
                ),
            )
            defer span.End()

            defer func() {
                recovered := recover()
                if nil == recovered {
                    return
                }

                span.SetStatus(codes.Error, "handler panicked")
                panic(recovered)
            }()

            tracedRuntime := runtime.New(spanContext, runtimeInstance.Scope(), runtimeInstance.Container())

            /* the writer is recorded, so a status the handler commits directly — the streaming, proxy and upgrade shapes return no response — is the one the span carries */
            recorder := &statusRecordingResponseWriter{ResponseWriter: writer, statusCode: nethttp.StatusOK}

            response, handlerErr := next(tracedRuntime, recorder, request)

            statusKnown := true == recorder.wroteHeader || nil != handlerErr || false == isNilResponse(response)
            statusCode := completedStatusCode(handlerErr, response, recorder)

            if true == statusKnown {
                span.SetAttributes(attribute.Int("http.response.status_code", statusCode))
            }

            message := ""
            if nil != handlerErr {
                /* the message is read through the exception package, under the recover its readers carry, because the error is the handler's own value and a typed nil of a pointer type answers Error() with a panic this middleware would then charge to itself */
                renderedMessage, isRendered := exception.LogContext(handlerErr)["error"].(string)
                message = renderedMessage
                if false == isRendered {
                    message = "the handler error could not be rendered"
                }

                recordSpanError(span, handlerErr, message)
            }

            /* a server span is in error for a failure of the server, read on the status the client receives: a deliberate sub-500 the handler answered — a 404, a 422 — is the client's, and a handler error after a committed success keeps the success while the error stays recorded as an event */
            if true == statusKnown && nethttp.StatusInternalServerError <= statusCode {
                description := nethttp.StatusText(statusCode)
                if nil != handlerErr {
                    description = message
                }

                span.SetStatus(codes.Error, description)
            }

            return response, handlerErr
        }
    }
}

/* recordSpanError records the handler error as the span's exception event, and records its rendered message instead when the recording itself panics — the sdk asks the error for its text, which a typed nil cannot give */
func recordSpanError(span trace.Span, handlerErr error, message string) {
    defer func() {
        if nil != recover() {
            span.RecordError(errors.New(message))
        }
    }()

    span.RecordError(handlerErr)
}

func spanName(request httpcontract.Request) string {
    return normalizedMethod(request.HttpRequest().Method) + " " + routeLabel(request)
}
