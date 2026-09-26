package http

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func PrefersHtml(request httpcontract.Request) bool {
    /* every line of a repeated Accept field is joined through the door the error renderer uses, so both readers of one response see one preference */
    acceptHeader := joinedAcceptHeader(request)
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

/* acceptQuality reports the weight the Accept header gives a media type and where it was named, honouring q and a refusal with q=0; a wildcard range supplies the weight when the exact type is absent. It answers -1 when nothing matches. */
func acceptQuality(acceptHeader string, mediaType string) (float64, int) {
    quality := -1.0
    position := -1
    specificity := -1

    slashIndex := strings.IndexByte(mediaType, '/')
    typeWildcard := mediaType[:slashIndex+1] + "*"

    /* members and parameters split outside quoted sections, the serializer reader's grammar, and a header the member cap cut reads as unparsable, since a member past the cap may carry a refusal */
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

        /* a q outside the RFC 7231 qvalue grammar drops the member, the serializer reader's rule */
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

        /* the most specific matching range supplies the weight, so a wildcard never overrides an exact type's q; equal specificity takes the higher q */
        if entrySpecificity > specificity || (entrySpecificity == specificity && entryQuality > quality) {
            specificity = entrySpecificity
            quality = entryQuality
            position = entryIndex
        }
    }

    return quality, position
}
