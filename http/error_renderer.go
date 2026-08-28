package http

import (
    "fmt"
    "html"
    nethttp "net/http"
    "strings"
    "time"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/serializer"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

/* renderErrorResponse builds the framework's error response, and it is the one door every default rendering goes through — the exception listener and each of the kernel's own fallback paths — so a request is answered in the same shape whichever of them renders it.

   The body honours the same negotiation the success path honours: a client that prefers html gets the html error page, anything else goes through the serializer manager resolved against the joined Accept lines. The one asymmetry is deliberate and fail-closed: an Accept header that refuses every available media type keeps the error's own status code and is served the default json body, where the success path answers 406 — an error status is the signal itself, and masking a security refusal behind not-acceptable would hide it from the client that has to react to it. A runtime without a serializer manager, a serializer that fails, or a payload that cannot be marshalled all fall back to the default json body under the same rule: an error response always exists.

   The base payload carries the error message and the moment; entries of payloadExtras are carried next to them, and the base keys win a collision so the rendered error always names itself. */
func renderErrorResponse(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    statusCode int,
    message string,
    payloadExtras map[string]any,
) httpcontract.Response {
    requestId := requestIdFromRequest(request)

    var response httpcontract.Response

    if true == PrefersHtml(request) {
        htmlBody := "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body>" +
            "<h1>Error</h1>" +
            "<p>Status: " + fmt.Sprintf("%d", statusCode) + "</p>" +
            "<p>Message: " + html.EscapeString(message) + "</p>" +
            "<p>Request-Id: " + html.EscapeString(requestId) + "</p>" +
            "</body></html>"

        response = HtmlResponse(statusCode, htmlBody)
    } else {
        payload := make(map[string]any, len(payloadExtras)+2)
        for key, value := range payloadExtras {
            payload[key] = value
        }
        payload["error"] = message
        payload["time"] = time.Now().Format(time.RFC3339)

        response = renderNegotiatedErrorPayload(runtimeInstance, request, statusCode, message, payload)
    }

    if "" != requestId && "" == response.Headers().Get(HeaderRequestId) {
        response.Headers().Set(HeaderRequestId, requestId)
    }

    return response
}

func renderNegotiatedErrorPayload(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    statusCode int,
    message string,
    payload map[string]any,
) httpcontract.Response {
    /* the manager is resolved quietly, not through the soft resolver that logs its failure: a runtime without a serializer manager is a legitimate configuration whose error bodies simply stay json, and an error line per rendered error would let a refused request write to the log through the very absence the fallback exists for */
    serializerManager := (*serializer.SerializerManager)(nil)
    if nil != runtimeInstance {
        serializerManager, _ = runtime.FromRuntime[*serializer.SerializerManager](runtimeInstance, serializer.ServiceSerializerManager)
    }

    if nil == serializerManager {
        return jsonErrorResponseFromPayload(statusCode, message, payload)
    }

    /* every resolution failure falls back the same way, the not-acceptable refusal among them: the
    asymmetry the door's own documentation states is that an error keeps its status rather than being
    masked behind a 406, so naming that refusal separately here would have claimed a branch it does not
    have — it was strictly covered by the error test beside it. The success path in result_handler is
    where the two are told apart, and it also records the failure the error path deliberately keeps quiet.
    The serializer test is defence in depth: this manager answers a serializer or an error, never neither,
    and a regression that broke that is already caught by the recover around the serialize call, which
    returns to this same fallback. */
    serializerInstance, err := serializerManager.ResolveByAcceptHeader(joinedAcceptHeader(request))
    if nil != err || nil == serializerInstance {
        return jsonErrorResponseFromPayload(statusCode, message, payload)
    }

    serializedBytes, serializeErr := serializeErrorPayloadSafely(serializerInstance, payload)
    if nil != serializeErr {
        return jsonErrorResponseFromPayload(statusCode, message, payload)
    }

    response := NewResponse(statusCode, serializedBytes)
    if nil == response.headers {
        response.headers = make(nethttp.Header)
    }
    response.headers.Set("Content-Type", serializerInstance.ContentType())

    return response
}

func jsonErrorResponseFromPayload(statusCode int, message string, payload map[string]any) httpcontract.Response {
    jsonResponse, jsonErr := JsonResponse(statusCode, payload)
    if nil == jsonErr {
        return jsonResponse
    }

    return JsonErrorResponse(statusCode, message)
}

/* joinedAcceptHeader reads the accept header the way the success path reads it: every line joined before parsing, because Get answers only the first line of a repeated field and the accept header is list-typed — a refusal the client sent on a second line would otherwise vanish. */
func joinedAcceptHeader(request httpcontract.Request) string {
    if true == internal.IsNilInterface(request) {
        return ""
    }

    httpRequest := request.HttpRequest()
    if nil == httpRequest || nil == httpRequest.Header {
        return ""
    }

    return strings.Join(httpRequest.Header.Values("Accept"), ", ")
}

func requestIdFromRequest(request httpcontract.Request) string {
    if true == internal.IsNilInterface(request) {
        return ""
    }

    requestContext := request.RequestContext()
    if nil == requestContext {
        return ""
    }

    return requestContext.RequestId()
}

/* serializeErrorPayloadSafely contains a serializer that panics: the renderer is consulted from inside the kernel's recovery defer, where an application serializer dying on the error payload would raise a second panic past the recovery and reset the connection — against the door's own promise that an error response always exists. A panic is answered as the error the caller already degrades on, and the json fallback serves the page. */
func serializeErrorPayloadSafely(serializerInstance serializercontract.Serializer, payload any) (serializedBytes []byte, serializeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        serializedBytes = nil
        serializeErr = exception.NewError(
            "error payload serialization panicked",
            exceptioncontract.Context{
                "value": fmt.Sprintf("%v", recoveredValue),
            },
            nil,
        )
    }()

    return serializerInstance.Serialize(payload)
}
