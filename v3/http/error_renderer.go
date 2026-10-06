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
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

/* renderErrorResponse builds the framework's error response, the one door every default rendering goes through. The body is negotiated as on the success path, except that an Accept refusing every media type keeps the error's status and is served the default json body: the error status is the signal, so no 406 masks it. Any serializer failure falls back to that json body, so an error response always exists. The body is the standardized error envelope; payloadExtras entries join it at the top level, where the envelope's own keys win, and context and cause join the error object. */
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
            "message": message,
        }

        payload := make(map[string]any, len(payloadExtras)+4)
        for key, value := range payloadExtras {
            if "context" == key || "cause" == key {
                errorObject[key] = value

                continue
            }

            payload[key] = value
        }

        payload["status"] = statusCode
        payload["time"] = time.Now().Format(time.RFC3339)
        payload["error"] = errorObject
        if "" != requestId {
            payload["requestId"] = requestId
        }

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

const (
    maxPlainTextDepth = 8
    maxPlainTextLines = 1024
    maxPlainTextBytes   = 256 * 1024
    maxPlainTextMembers = 16 * 1024
)

/* plainTextErrorResponse writes the error envelope for a text/plain client as lines: the status and the message first, then the request id, the time and every other entry in key order, a map indented beneath its key and a list one item per line. A plain-text serializer has no shape for a map and would print a Go map dump. */
func plainTextErrorResponse(statusCode int, message string, payload map[string]any, contentType string) httpcontract.Response {
    lines := []string{fmt.Sprintf("%d %s", statusCode, message)}
    budget := &plainTextBudget{remainingBytes: maxPlainTextBytes, remainingMembers: maxPlainTextMembers}

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

        lines = appendPlainTextEntry(lines, "", leadingKey, value, 0, budget)
        delete(remaining, leadingKey)
    }

    keys := make([]string, 0, len(remaining))
    for key := range remaining {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
        lines = appendPlainTextEntry(lines, "", key, remaining[key], 0, budget)
    }

    if maxPlainTextLines < len(lines) {
        lines = append(lines[:maxPlainTextLines], "...(truncated)")
    } else if true == budget.exhausted() {
        lines = append(lines, "...(truncated)")
    }

    response := NewResponse(statusCode, []byte(strings.Join(lines, "\n")+"\n"))
    if nil == response.headers {
        response.headers = make(nethttp.Header)
    }
    response.headers.Set("Content-Type", contentType)

    return response
}

/* plainTextErrorResponseSafely contains a value whose String or Error panics, since the renderer runs inside the kernel's recovery defer; it answers false and the caller serves the json envelope instead */
func plainTextErrorResponseSafely(statusCode int, message string, payload map[string]any, contentType string) (response httpcontract.Response, rendered bool) {
    defer func() {
        if nil != recover() {
            response = nil
            rendered = false
        }
    }()

    return plainTextErrorResponse(statusCode, message, payload, contentType), true
}

/* appendPlainTextEntry writes one entry and the entries nested under it; a map deeper than maxPlainTextDepth, a map that holds itself among them, is written as an ellipsis, and the walk stops adding lines once the body holds more than maxPlainTextLines or has spent its budget */
func appendPlainTextEntry(lines []string, indent string, key string, value any, depth int, budget *plainTextBudget) []string {
    if maxPlainTextLines < len(lines) || true == budget.exhausted() {
        return lines
    }

    budget.count()
    label := indent + budget.spend(plainTextLabel(key)) + ":"

    if nil == value {
        return append(lines, label)
    }

    if maxPlainTextDepth <= depth {
        return append(lines, labelled(label, "..."))
    }

    switch typedValue := value.(type) {
    case string:
        return append(lines, labelled(label, budget.spend(typedValue)))
    case error:
        return append(lines, labelled(label, budget.spend(typedValue.Error())))
    case fmt.Stringer:
        return append(lines, labelled(label, budget.spend(typedValue.String())))
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
            lines = appendPlainTextEntry(lines, indent+"  ", mapKey, reflected.MapIndex(reflect.ValueOf(mapKey).Convert(reflected.Type().Key())).Interface(), depth+1, budget)
        }

        return lines
    case reflect.Slice, reflect.Array:
        if reflect.Uint8 == reflected.Type().Elem().Kind() {
            break
        }

        lines = append(lines, label)
        for index := 0; index < reflected.Len() && maxPlainTextLines >= len(lines) && false == budget.exhausted(); index++ {
            lines = append(lines, indent+"  - "+plainTextValue(reflected.Index(index), depth+1, budget))
        }

        return lines
    }

    return append(lines, labelled(label, plainTextValue(reflected, depth, budget)))
}

/* labelled writes an entry on its label's line, the bare label when the value is empty */
func labelled(label string, text string) string {
    if "" == text {
        return label
    }

    return label + " " + text
}

/* plainTextValue spells a value the walk does not lay out as entries. It descends maps, slices, arrays, structs, pointers and interfaces itself, to maxPlainTextDepth and maxPlainTextLines members each, because fmt has no cycle detection: a value that reaches itself through a slice or a struct would overflow the stack, which no recover catches. Every value it spells spends the body's budget, since the two bounds alone let a value reaching itself through many members be spelled a number of times exponential in the depth. */
func plainTextValue(reflected reflect.Value, depth int, budget *plainTextBudget) string {
    if false == reflected.IsValid() {
        return "<nil>"
    }

    if true == budget.exhausted() {
        return "..."
    }

    budget.count()

    if true == reflected.CanInterface() {
        switch typedValue := reflected.Interface().(type) {
        case string:
            return budget.spend(typedValue)
        case error:
            return budget.spend(typedValue.Error())
        case fmt.Stringer:
            return budget.spend(typedValue.String())
        }
    }

    switch reflected.Kind() {
    case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct, reflect.Pointer, reflect.Interface:
        if maxPlainTextDepth <= depth {
            return "..."
        }
    }

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Interface:
        if true == reflected.IsNil() {
            return "<nil>"
        }

        prefix := ""
        if reflect.Pointer == reflected.Kind() {
            prefix = "&"
        }

        return prefix + plainTextValue(reflected.Elem(), depth+1, budget)
    case reflect.Map:
        members := make([]string, 0, min(reflected.Len(), maxPlainTextLines))
        iterator := reflected.MapRange()
        for iterator.Next() {
            if maxPlainTextLines <= len(members) || true == budget.exhausted() {
                break
            }

            members = append(members, plainTextValue(iterator.Key(), depth+1, budget)+":"+plainTextValue(iterator.Value(), depth+1, budget))
        }
        sort.Strings(members)

        if len(members) < reflected.Len() {
            members = append(members, "...")
        }

        return "map[" + strings.Join(members, " ") + "]"
    case reflect.Slice, reflect.Array:
        if reflect.Uint8 == reflected.Type().Elem().Kind() {
            break
        }

        members := make([]string, 0, min(reflected.Len(), maxPlainTextLines+1))
        for index := 0; index < reflected.Len(); index++ {
            if maxPlainTextLines <= index || true == budget.exhausted() {
                members = append(members, "...")
                break
            }

            members = append(members, plainTextValue(reflected.Index(index), depth+1, budget))
        }

        return "[" + strings.Join(members, " ") + "]"
    case reflect.Struct:
        members := make([]string, 0, reflected.NumField())
        for index := 0; index < reflected.NumField(); index++ {
            if true == budget.exhausted() {
                members = append(members, "...")
                break
            }

            members = append(members, budget.spend(reflected.Type().Field(index).Name)+":"+plainTextValue(reflected.Field(index), depth+1, budget))
        }

        return "{" + strings.Join(members, " ") + "}"
    }

    /* every kind left is a scalar, a channel, a func or a byte sequence, none of which can reach another value */
    return budget.spend(fmt.Sprint(reflected))
}

/* plainTextBudget is the room one text/plain body has left: every value walked counts as a member, a value cut at the depth bound included, and every text spends its length, so the walk ends past maxPlainTextMembers or maxPlainTextBytes whatever the shape of what it walks */
type plainTextBudget struct {
    remainingBytes   int
    remainingMembers int
}

func (instance *plainTextBudget) count() {
    instance.remainingMembers--
}

/* spend answers the text and takes its length out of the budget; a text that does not fit is cut on a rune boundary and ends with an ellipsis, and nothing is answered once the budget is spent */
func (instance *plainTextBudget) spend(text string) string {
    if true == instance.exhausted() {
        return "..."
    }

    if len(text) <= instance.remainingBytes {
        instance.remainingBytes -= len(text)

        return text
    }

    cut := instance.remainingBytes
    for 0 < cut && false == utf8.RuneStart(text[cut]) {
        cut--
    }

    instance.remainingBytes = 0

    return text[:cut] + "..."
}

func (instance *plainTextBudget) exhausted() bool {
    return 0 >= instance.remainingBytes || 0 >= instance.remainingMembers
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

/* setResponseHeader sets one header on a rendered response, whose headers may be nil */
func setResponseHeader(response httpcontract.Response, name string, value string) {
    headers := response.Headers()
    if nil == headers {
        headers = make(nethttp.Header)
    }

    headers.Set(name, value)
    response.SetHeaders(headers)
}
