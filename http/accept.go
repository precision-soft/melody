package http

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
)

func PrefersHtml(request httpcontract.Request) bool {
    if true == internal.IsNilInterface(request) {
        return false
    }

    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return false
    }

    /* every line of a repeated Accept field is joined before parsing, as the error renderer joins them: the header is list-typed, so both readers of one error response see the same preference */
    acceptHeader := strings.Join(httpRequest.Header.Values("Accept"), ",")
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

/* acceptQuality reports the weight the Accept header gives a media type and where it was named. A client ranks alternatives with q, and q=0 refuses a type outright, so the header is read by weight rather than by position; a wildcard range supplies the weight when the exact type is absent. Returns -1 when nothing matches. */
func acceptQuality(acceptHeader string, mediaType string) (float64, int) {
    quality := -1.0
    position := -1
    specificity := -1

    slashIndex := strings.IndexByte(mediaType, '/')
    typeWildcard := mediaType[:slashIndex+1] + "*"

    /* members and parameters split outside quoted sections, the serializer reader's grammar: a bare split would cut through a quoted parameter value and lose the refusal a q=0 after it carries */
    /* a header the member cap cut is read as unparsable, so nothing matches: the members past the cap can carry a refusal (text/html;q=0) that a wildcard before it does not */
    entries, cut := internal.SplitOutsideQuotes(acceptHeader, ',')
    if true == cut {
        return quality, position
    }

    for entryIndex, entry := range entries {
        parameters, cut := internal.SplitOutsideQuotes(entry, ';')
        if true == cut {
            return -1.0, -1
        }

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

        /* a member whose q falls outside the RFC 7231 qvalue grammar is dropped whole, the serializer reader's rule, so q=Inf and q=NaN weigh nothing and the two negotiators of one response agree */
        entryQuality := 1.0
        entryQualityValid := true
        for _, parameter := range parameters[1:] {
            trimmed := strings.TrimSpace(parameter)
            if false == strings.HasPrefix(strings.ToLower(trimmed), "q=") {
                continue
            }

            parsed, valid := internal.ParseQualityValue(strings.TrimSpace(trimmed[2:]))
            if false == valid {
                entryQualityValid = false

                continue
            }

            entryQuality = parsed
        }

        if false == entryQualityValid {
            continue
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
