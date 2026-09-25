package serializer

import (
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/internal"
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

/* parseAcceptHeader drops a member whose q falls outside the RFC 7231 qvalue grammar rather than guessing a weight. */
func parseAcceptHeader(acceptHeader string) ([]acceptedMime, bool) {
    /* a header the member cap cut is reported, and the manager refuses it as not acceptable, since a member past the cap may carry a refusal */
    parts, cut := internal.SplitOutsideQuotes(acceptHeader, ',')
    if true == cut {
        return nil, true
    }

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
                parameters, cut := internal.SplitOutsideQuotes(parametersPart, ';')
                if true == cut {
                    return nil, true
                }

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

    return result, false
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

/* acceptMatchSpecificity keeps q=0 in the parsed list as a refusal, so a candidate it covers is excluded rather than served as the default. */
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
