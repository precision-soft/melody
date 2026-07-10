package http

import (
    "strconv"
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func PrefersHtml(request httpcontract.Request) bool {
    if nil == request {
        return false
    }

    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return false
    }

    acceptHeader := httpRequest.Header.Get("Accept")
    if "" == acceptHeader {
        return false
    }

    htmlQuality, htmlPosition := acceptQuality(acceptHeader, "text/html")
    if 0 >= htmlQuality {
        /* absent, or explicitly refused with q=0 */
        return false
    }

    jsonQuality, jsonPosition := acceptQuality(acceptHeader, "application/json")
    if 0 >= jsonQuality {
        return true
    }

    if htmlQuality != jsonQuality {
        return htmlQuality > jsonQuality
    }

    /* equal weights: the order the client wrote them in is the only preference left to honour */
    return htmlPosition < jsonPosition
}

/* acceptQuality reports the weight the Accept header gives a media type and where it was named. A client ranks alternatives with the q parameter — "text/html;q=0.1, application/json" asks for json, and q=0 refuses a type outright — so reading the header by substring position alone serves a representation the client down-weighted or rejected. A wildcard range (a type wildcard, or the catch-all range) supplies the weight when the exact type is absent. Returns a quality of -1 when nothing matches. */
func acceptQuality(acceptHeader string, mediaType string) (float64, int) {
    quality := -1.0
    position := -1

    slashIndex := strings.IndexByte(mediaType, '/')
    typeWildcard := mediaType[:slashIndex+1] + "*"

    for entryIndex, entry := range strings.Split(acceptHeader, ",") {
        parameters := strings.Split(entry, ";")
        mediaRange := strings.ToLower(strings.TrimSpace(parameters[0]))

        if mediaRange != mediaType && mediaRange != typeWildcard && "*/*" != mediaRange {
            continue
        }

        entryQuality := 1.0
        for _, parameter := range parameters[1:] {
            trimmed := strings.TrimSpace(parameter)
            if false == strings.HasPrefix(strings.ToLower(trimmed), "q=") {
                continue
            }

            parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(trimmed[2:]), 64)
            if nil != parseErr {
                continue
            }

            entryQuality = parsed
        }

        /* the most specific range wins ties, so an exact match never loses to a catch-all range that follows it */
        if entryQuality > quality || (entryQuality == quality && mediaRange == mediaType) {
            quality = entryQuality
            position = entryIndex
        }
    }

    return quality, position
}
