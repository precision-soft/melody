package opentelemetry

import (
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

            recorder := &statusRecordingResponseWriter{ResponseWriter: writer, statusCode: nethttp.StatusOK}
            response, handlerErr := next(tracedRuntime, recorder, request)

            if true == recorder.wroteHeader {
                statusCode := recorder.statusCode
                if true == recorder.hijacked {
                    statusCode = nethttp.StatusSwitchingProtocols
                }
                span.SetAttributes(attribute.Int("http.response.status_code", statusCode))
                if 500 <= statusCode {
                    span.SetStatus(codes.Error, nethttp.StatusText(statusCode))
                }
            } else if false == isNilResponse(response) {
                span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode()))
                if 500 <= response.StatusCode() {
                    span.SetStatus(codes.Error, nethttp.StatusText(response.StatusCode()))
                }
            } else if nil != handlerErr {

                span.SetAttributes(attribute.Int("http.response.status_code", statusCodeForError(handlerErr)))
            }

            if nil != handlerErr {
                span.RecordError(handlerErr)
                span.SetStatus(codes.Error, handlerErr.Error())
            }

            return response, handlerErr
        }
    }
}

func spanName(request httpcontract.Request) string {
    return normalizedMethod(request.HttpRequest().Method) + " " + routeLabel(request)
}
