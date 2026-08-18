package serializer

import (
    "sort"
    "strings"

    "github.com/precision-soft/melody/internal"
)

const (
    MimeApplicationJson = "application/json"
    MimeTextPlain       = "text/plain"
)

func normalizeMime(mime string) string {
    mime = strings.TrimSpace(mime)
    mime = strings.ToLower(mime)

    separatorIndex := strings.Index(mime, ";")
    if -1 != separatorIndex {
        mime = strings.TrimSpace(mime[:separatorIndex])
    }

    return mime
}

type acceptedMime struct {
    mime         string
    qualityValue float64
}

/* a member whose q parameter falls outside the RFC 7231 qvalue grammar is dropped whole rather than rounded to a guess: the previous leniency scored an unparseable q as full acceptance, clamped a negative one into a refusal and let NaN through as a weight that no comparison could select or refuse, so the same malformed header could open, close or silently poison the negotiation depending on the spelling */
func parseAcceptHeader(acceptHeader string) []acceptedMime {
    parts := internal.SplitOutsideQuotes(acceptHeader, ',')
    result := make([]acceptedMime, 0, len(parts))

    for _, part := range parts {
        part = strings.TrimSpace(part)
        if "" == part {
            continue
        }

        mimePart := part
        qualityValue := 1.0
        qualityInvalid := false

        parameterSeparatorIndex := strings.Index(part, ";")
        if -1 != parameterSeparatorIndex {
            mimePart = strings.TrimSpace(part[:parameterSeparatorIndex])
            parametersPart := strings.TrimSpace(part[parameterSeparatorIndex+1:])

            if "" != parametersPart {
                parameters := internal.SplitOutsideQuotes(parametersPart, ';')
                for _, parameter := range parameters {
                    parameter = strings.TrimSpace(parameter)
                    if "" == parameter {
                        continue
                    }

                    keyValue := strings.SplitN(parameter, "=", 2)
                    if 2 != len(keyValue) {
                        continue
                    }

                    key := strings.TrimSpace(strings.ToLower(keyValue[0]))
                    value := strings.TrimSpace(keyValue[1])

                    if "q" == key {
                        parsedValue, valid := internal.ParseQualityValue(value)
                        if false == valid {
                            qualityInvalid = true

                            continue
                        }

                        qualityValue = parsedValue
                    }
                }
            }
        }

        if true == qualityInvalid {
            continue
        }

        mimePart = normalizeMime(mimePart)
        if "" == mimePart {
            continue
        }

        if "*/*" == mimePart {
            result = append(result, acceptedMime{
                mime:         "*/*",
                qualityValue: qualityValue,
            })
            continue
        }

        result = append(result, acceptedMime{
            mime:         mimePart,
            qualityValue: qualityValue,
        })
    }

    sort.SliceStable(result, func(i int, j int) bool {
        return result[i].qualityValue > result[j].qualityValue
    })

    return result
}

func isWildcardSubtype(mime string) bool {
    return true == strings.HasSuffix(mime, "/*") && false == strings.HasPrefix(mime, "*")
}

func matchWildcardSubtype(wildcardMime string, candidateMime string) bool {
    wildcardMime = normalizeMime(wildcardMime)
    candidateMime = normalizeMime(candidateMime)

    if false == isWildcardSubtype(wildcardMime) {
        return false
    }

    prefix := strings.TrimSuffix(wildcardMime, "*")
    return true == strings.HasPrefix(candidateMime, prefix)
}

/* a q of 0 is a refusal, not an absence: it is kept in the parsed list so a candidate it covers can be excluded rather than falling through to the default */
func acceptMatchSpecificity(acceptedMimeValue string, candidateMime string) int {
    acceptedMimeValue = normalizeMime(acceptedMimeValue)
    candidateMime = normalizeMime(candidateMime)

    if acceptedMimeValue == candidateMime {
        return 3
    }

    if true == matchWildcardSubtype(acceptedMimeValue, candidateMime) {
        return 2
    }

    if "*/*" == acceptedMimeValue {
        return 1
    }

    return 0
}

func acceptQualityFor(acceptedMimes []acceptedMime, candidateMime string) (float64, int, bool) {
    bestSpecificity := 0
    bestQuality := 0.0

    for _, acceptedMimeValue := range acceptedMimes {
        specificity := acceptMatchSpecificity(acceptedMimeValue.mime, candidateMime)
        if specificity > bestSpecificity {
            bestSpecificity = specificity
            bestQuality = acceptedMimeValue.qualityValue
        }
    }

    if 0 == bestSpecificity {
        return 0, 0, false
    }

    return bestQuality, bestSpecificity, true
}
