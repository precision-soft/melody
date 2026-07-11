package http

import (
    "strconv"
    "strings"

    httpcontract "github.com/precision-soft/melody/http/contract"
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
    specificity := -1

    slashIndex := strings.IndexByte(mediaType, '/')
    typeWildcard := mediaType[:slashIndex+1] + "*"

    for entryIndex, entry := range strings.Split(acceptHeader, ",") {
        parameters := strings.Split(entry, ";")
        mediaRange := strings.ToLower(strings.TrimSpace(parameters[0]))

        entrySpecificity := -1
        if mediaRange == mediaType {
            entrySpecificity = 2
        } else if mediaRange == typeWildcard {
            entrySpecificity = 1
        } else if "*/*" == mediaRange {
            entrySpecificity = 0
        } else {
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

        /* the most specific matching range supplies the weight: a more specific match replaces a less specific one outright (so a wildcard can never override an exact type's q, including an explicit q=0 refusal), and equal-specificity ties fall to the higher q */
        if entrySpecificity > specificity || (entrySpecificity == specificity && entryQuality > quality) {
            specificity = entrySpecificity
            quality = entryQuality
            position = entryIndex
        }
    }

    return quality, position
}
