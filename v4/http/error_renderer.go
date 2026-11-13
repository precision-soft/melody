package http

import (
    "fmt"
    "html"
    "mime"
    nethttp "net/http"
    "sort"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/plaintext"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
)

/* renderErrorResponse builds the framework's error response, the one door every default rendering goes through. The body is negotiated as on the success path, except that an Accept refusing every media type keeps the error's status and is served the default json body: the error status is the signal, so no 406 masks it. Any serializer failure falls back to that json body, so an error response always exists. The body is the error envelope, the status, the moment and the request id beside the error object that carries the message; the payloadExtras entries an error object holds, the client-facing code, key and parameters and the debug context and cause, join that object, and every other entry joins the body at the top level, where the envelope's own keys win. */
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
        errorObject := map[string]any{
            errorResponseKeyMessage: message,
        }

        payload := make(map[string]any, len(payloadExtras)+4)
        for key, value := range payloadExtras {
            if true == isErrorObjectKey(key) {
                errorObject[key] = value

                continue
            }

            payload[key] = value
        }

        payload[errorResponseKeyStatus] = statusCode
        payload[errorResponseKeyTime] = time.Now().Format(time.RFC3339)
        payload[errorResponseKeyError] = errorObject
        if "" != requestId {
            payload[errorResponseKeyRequestId] = requestId
        }

        response = renderNegotiatedErrorPayload(runtimeInstance, request, statusCode, message, payload)
    }

    if "" != requestId && "" == response.Headers().Get(HeaderRequestId) {
        response.Headers().Set(HeaderRequestId, requestId)
    }

    return response
}

func isErrorObjectKey(key string) bool {
    switch key {
    case errorResponseKeyCode, errorResponseKeyKey, errorResponseKeyParameters, errorResponseKeyContext, errorResponseKeyCause:
        return true
    }

    return false
}

func renderNegotiatedErrorPayload(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    statusCode int,
    message string,
    payload map[string]any,
) httpcontract.Response {
    /* resolved quietly: a runtime without a serializer manager keeps its error bodies json, and a record per rendered error would let a refused request write to the log */
    serializerManager := (*serializer.SerializerManager)(nil)
    if nil != runtimeInstance {
        serializerManager, _ = runtime.FromRuntime[*serializer.SerializerManager](runtimeInstance, serializer.ServiceSerializerManager)
    }

    if nil == serializerManager {
        return jsonErrorResponseFromPayload(statusCode, message, payload)
    }

    /* every resolution failure falls back the same way, the not-acceptable refusal included */
    serializerInstance, err := serializerManager.ResolveByAcceptHeader(joinedAcceptHeader(request))
    if nil != err || nil == serializerInstance {
        return jsonErrorResponseFromPayload(statusCode, message, payload)
    }

    if true == isPlainTextMediaType(serializerInstance.ContentType()) {
        if response, rendered := plainTextErrorResponseSafely(statusCode, message, payload, serializerInstance.ContentType()); true == rendered {
            return response
        }

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

/* joinedAcceptHeader joins every Accept line before parsing, as the success path does. */
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

/* serializeErrorPayloadSafely contains a serializer that panics, since the renderer runs inside the kernel's recovery defer; the panic is answered as an error and the json fallback serves the page. */
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
                "value": internal.DescribeRecoveredValue(recoveredValue),
            },
            nil,
        )
    }()

    return serializerInstance.Serialize(payload)
}

/* isPlainTextMediaType answers whether a serializer's content type is text/plain, whatever its parameters, so an application's own text serializer takes the same door as the framework's. */
func isPlainTextMediaType(contentType string) bool {
    mediaType, _, err := mime.ParseMediaType(contentType)
    if nil != err {
        return false
    }

    return "text/plain" == mediaType
}

/* plainTextErrorResponse writes the error envelope for a text/plain client as lines: the status and the message first, then the request id, the time and every other entry in key order, the error object's own entries among them, written as the plain-text serializer writes a value, its keys spelled as words. The message is one line, a control character in it spelled as its escape, as the walk spells it in every entry. */
func plainTextErrorResponse(statusCode int, message string, payload map[string]any, contentType string) httpcontract.Response {
    options := plaintext.DefaultOptions()
    options.LabelKeys = true
    walk := plaintext.NewWalk(options, []string{fmt.Sprintf("%d %s", statusCode, internal.EscapeControlCharacters(message))})

    remaining := make(map[string]any, len(payload))
    for key, value := range payload {
        remaining[key] = value
    }
    delete(remaining, errorResponseKeyStatus)

    /* the error object carries the message the first line already names; its other entries are listed beside the envelope's */
    if errorObject, isMap := remaining[errorResponseKeyError].(map[string]any); true == isMap {
        delete(remaining, errorResponseKeyError)
        for key, value := range errorObject {
            if errorResponseKeyMessage == key {
                continue
            }

            remaining[key] = value
        }
    }

    for _, leadingKey := range []string{errorResponseKeyRequestId, errorResponseKeyTime} {
        value, exists := remaining[leadingKey]
        if false == exists {
            continue
        }

        walk.Entry(leadingKey, value)
        delete(remaining, leadingKey)
    }

    keys := make([]string, 0, len(remaining))
    for key := range remaining {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
        walk.Entry(key, remaining[key])
    }

    response := NewResponse(statusCode, []byte(strings.Join(walk.Lines(), "\n")+"\n"))
    if nil == response.headers {
        response.headers = make(nethttp.Header)
    }
    response.headers.Set("Content-Type", contentType)

    return response
}

/* plainTextErrorResponseSafely contains a value whose String or Error panics, since the renderer runs inside the kernel's recovery defer; it answers false and the caller serves the json body instead */
func plainTextErrorResponseSafely(statusCode int, message string, payload map[string]any, contentType string) (response httpcontract.Response, rendered bool) {
    defer func() {
        if nil != recover() {
            response = nil
            rendered = false
        }
    }()

    return plainTextErrorResponse(statusCode, message, payload, contentType), true
}

/* setResponseHeader sets one header on a rendered response, whose headers may be nil */
func setResponseHeader(response httpcontract.Response, name string, value string) {
    headers := response.Headers()
    if nil == headers {
        headers = make(nethttp.Header)
    }

    headers.Set(name, value)
    response.SetHeaders(headers)
}
