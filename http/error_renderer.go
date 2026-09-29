package http

import (
    "fmt"
    "html"
    "mime"
    nethttp "net/http"
    "reflect"
    "sort"
    "strings"
    "time"
    "unicode"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/serializer"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
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

    if true == isPlainTextMediaType(serializerInstance.ContentType()) {
        return plainTextErrorResponse(statusCode, message, payload, serializerInstance.ContentType())
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

/* isPlainTextMediaType answers whether a serializer's content type is text/plain, whatever its parameters, so an application's own text serializer takes the same door as the framework's. */
func isPlainTextMediaType(contentType string) bool {
    mediaType, _, err := mime.ParseMediaType(contentType)
    if nil != err {
        return false
    }

    return "text/plain" == mediaType
}

/* plainTextErrorResponse writes the error envelope for a text/plain client as lines: the status and the message first, then the request id, the time and every other entry in key order, a map indented beneath its key and a list one item per line. A plain-text serializer has no shape for a map and would print a Go map dump. */
func plainTextErrorResponse(statusCode int, message string, payload map[string]any, contentType string) httpcontract.Response {
    lines := []string{fmt.Sprintf("%d %s", statusCode, message)}

    remaining := make(map[string]any, len(payload))
    for key, value := range payload {
        remaining[key] = value
    }
    delete(remaining, "status")

    /* the error object carries the message the first line already names; its other entries, the debug context and cause, are listed beside the envelope's */
    if errorObject, isMap := remaining["error"].(map[string]any); true == isMap {
        delete(remaining, "error")
        for key, value := range errorObject {
            if "message" == key {
                continue
            }

            remaining[key] = value
        }
    } else if errorMessage, isString := remaining["error"].(string); true == isString && errorMessage == message {
        delete(remaining, "error")
    }

    for _, leadingKey := range []string{"requestId", "time"} {
        value, exists := remaining[leadingKey]
        if false == exists {
            continue
        }

        lines = appendPlainTextEntry(lines, "", leadingKey, value)
        delete(remaining, leadingKey)
    }

    keys := make([]string, 0, len(remaining))
    for key := range remaining {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
        lines = appendPlainTextEntry(lines, "", key, remaining[key])
    }

    response := NewResponse(statusCode, []byte(strings.Join(lines, "\n")+"\n"))
    if nil == response.headers {
        response.headers = make(nethttp.Header)
    }
    response.headers.Set("Content-Type", contentType)

    return response
}

func appendPlainTextEntry(lines []string, indent string, key string, value any) []string {
    label := indent + plainTextLabel(key) + ":"

    if nil == value {
        return append(lines, label)
    }

    switch typedValue := value.(type) {
    case string:
        return append(lines, labelled(label, typedValue))
    case error:
        return append(lines, labelled(label, typedValue.Error()))
    case fmt.Stringer:
        return append(lines, labelled(label, typedValue.String()))
    }

    reflected := reflect.ValueOf(value)
    switch reflected.Kind() {
    case reflect.Map:
        if reflect.String != reflected.Type().Key().Kind() {
            break
        }

        lines = append(lines, label)

        mapKeys := make([]string, 0, reflected.Len())
        for _, mapKey := range reflected.MapKeys() {
            mapKeys = append(mapKeys, mapKey.String())
        }
        sort.Strings(mapKeys)

        for _, mapKey := range mapKeys {
            lines = appendPlainTextEntry(lines, indent+"  ", mapKey, reflected.MapIndex(reflect.ValueOf(mapKey).Convert(reflected.Type().Key())).Interface())
        }

        return lines
    case reflect.Slice, reflect.Array:
        if reflect.Uint8 == reflected.Type().Elem().Kind() {
            break
        }

        lines = append(lines, label)
        for index := 0; index < reflected.Len(); index++ {
            lines = append(lines, indent+"  - "+plainTextItem(reflected.Index(index).Interface()))
        }

        return lines
    }

    return append(lines, labelled(label, fmt.Sprint(value)))
}

/* labelled writes an entry on its label's line, the bare label when the value is empty */
func labelled(label string, text string) string {
    if "" == text {
        return label
    }

    return label + " " + text
}

func plainTextItem(value any) string {
    switch typedValue := value.(type) {
    case string:
        return typedValue
    case error:
        return typedValue.Error()
    case fmt.Stringer:
        return typedValue.String()
    }

    return fmt.Sprint(value)
}

/* plainTextLabel spells a camel-cased envelope key as words: requestId reads request id. */
func plainTextLabel(key string) string {
    var builder strings.Builder
    for index, character := range key {
        if 0 < index && true == unicode.IsUpper(character) {
            builder.WriteRune(' ')
        }
        builder.WriteRune(unicode.ToLower(character))
    }

    return builder.String()
}
