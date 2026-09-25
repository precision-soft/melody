package http

import (
    "fmt"
    "html"
    nethttp "net/http"
    "strings"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/serializer"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

/* renderErrorResponse builds the framework's error response; the exception listener and every kernel fallback render through it, so a request is answered in one shape. The body honours the negotiation the success path honours, with one deliberate fail-closed asymmetry: an Accept header that refuses every available type keeps the error's status and gets the default json body, where the success path answers 406. A missing serializer manager, a failing serializer or an unmarshallable payload fall back to that json body too, and the base keys win over payloadExtras. */
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

    /* every resolution failure, the not-acceptable refusal included, falls back the same way, because an error keeps its status rather than being masked behind a 406. The serializer check is defence in depth: the recover around the serialize call returns to this same fallback. */
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

/* joinedAcceptHeader reads the accept header the way the success path reads it: every line joined, because Get answers only the first line of a repeated field and a refusal on a second line would otherwise vanish. */
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
