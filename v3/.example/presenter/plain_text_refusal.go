package presenter

import (
    "fmt"
    "mime"
    "reflect"
    "sort"
    "strings"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* renderEnvelopeWith serializes an envelope through the negotiated serializer, except a refusal for a text/plain client, which is written as lines: the plain-text serializer has no shape for the envelope and would print its fields bare. A success keeps the serializer's rendering. */
func renderEnvelopeWith(
    statusCode int,
    payload apiResponse,
    serializerInstance serializerInstance,
) melodyhttpcontract.Response {
    if false == payload.Success && true == isPlainTextMediaType(serializerInstance.ContentType()) {
        return plainTextRefusal(statusCode, payload, serializerInstance.ContentType())
    }

    return serializeWith(statusCode, payload, serializerInstance)
}

func isPlainTextMediaType(contentType string) bool {
    mediaType, _, err := mime.ParseMediaType(contentType)
    if nil != err {
        return false
    }

    return "text/plain" == mediaType
}

/* plainTextRefusal writes the status and the public errors first, then what the refusal's payload tells the client to present next, then the request id, the time and the rest of the context in key order, and the debug trace one frame per line. */
func plainTextRefusal(statusCode int, payload apiResponse, contentType string) melodyhttpcontract.Response {
    lines := []string{fmt.Sprintf("%d %s", statusCode, strings.Join(payload.Errors, "; "))}

    /* a refusal that tells the client how to go on carries it in its payload, which the text/plain client reads beneath the status line as the json client reads it in the envelope */
    if refusalPayload, isMap := payload.Payload.(map[string]any); true == isMap {
        payloadKeys := make([]string, 0, len(refusalPayload))
        for key := range refusalPayload {
            payloadKeys = append(payloadKeys, key)
        }
        sort.Strings(payloadKeys)

        for _, key := range payloadKeys {
            lines = appendRefusalEntry(lines, "", key, refusalPayload[key])
        }
    }

    remaining := make(map[string]any, len(payload.Context))
    for key, value := range payload.Context {
        remaining[key] = value
    }
    delete(remaining, "statusCode")

    for _, leadingKey := range []string{"requestId", "time"} {
        value, exists := remaining[leadingKey]
        if false == exists {
            continue
        }

        lines = appendRefusalEntry(lines, "", leadingKey, value)
        delete(remaining, leadingKey)
    }

    keys := make([]string, 0, len(remaining))
    for key := range remaining {
        keys = append(keys, key)
    }
    sort.Strings(keys)

    for _, key := range keys {
        lines = appendRefusalEntry(lines, "", key, remaining[key])
    }

    if 0 < len(payload.Trace) {
        lines = append(lines, "trace:")
        for _, frame := range payload.Trace {
            lines = append(lines, "  - "+fmt.Sprintf("%v (%v)", frame["message"], frame["type"]))
        }
    }

    response := melodyhttp.NewResponse(statusCode, []byte(strings.Join(lines, "\n")+"\n"))
    response.Headers().Set("Content-Type", contentType)

    return response
}

func appendRefusalEntry(lines []string, indent string, key string, value any) []string {
    label := indent + refusalLabel(key) + ":"

    if nil == value {
        return append(lines, label)
    }

    if text, isString := value.(string); true == isString {
        return append(lines, labelled(label, text))
    }

    reflected := reflect.ValueOf(value)
    if reflect.Map == reflected.Kind() && reflect.String == reflected.Type().Key().Kind() {
        if 0 == reflected.Len() {
            return append(lines, label)
        }

        lines = append(lines, label)

        mapKeys := make([]string, 0, reflected.Len())
        for _, mapKey := range reflected.MapKeys() {
            mapKeys = append(mapKeys, mapKey.String())
        }
        sort.Strings(mapKeys)

        for _, mapKey := range mapKeys {
            lines = appendRefusalEntry(lines, indent+"  ", mapKey, reflected.MapIndex(reflect.ValueOf(mapKey).Convert(reflected.Type().Key())).Interface())
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

/* refusalLabel spells a camel-cased context key as words: routePattern reads route pattern. */
func refusalLabel(key string) string {
    var builder strings.Builder
    for index, character := range key {
        if 0 < index && 'A' <= character && 'Z' >= character {
            builder.WriteRune(' ')
            builder.WriteRune(character - 'A' + 'a')

            continue
        }
        builder.WriteRune(character)
    }

    return builder.String()
}
