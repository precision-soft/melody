package serializer

import (
    "sort"
    "strings"
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

/* splitOutsideQuotes splits value on separator while honouring quoted-string sections: a separator inside double quotes belongs to the parameter value it sits in, and a backslash escapes the character after it inside a quoted section, so a media range such as text/plain;version="1,2";q=0 stays one member instead of losing the refusal it carries. */
func splitOutsideQuotes(value string, separator byte) []string {
    result := make([]string, 0, 4)
    insideQuotes := false
    escaped := false
    start := 0

    for index := 0; index < len(value); index++ {
        character := value[index]

        if true == escaped {
            escaped = false

            continue
        }

        if true == insideQuotes && '\\' == character {
            escaped = true

            continue
        }

        if '"' == character {
            insideQuotes = false == insideQuotes

            continue
        }

        if separator == character && false == insideQuotes {
            result = append(result, value[start:index])
            start = index + 1
        }
    }

    return append(result, value[start:])
}

/* parseQualityValue validates value against the qvalue grammar of RFC 7231 — a zero with up to three decimal digits, or a one with up to three zero decimals — and reports anything outside it as invalid instead of guessing a weight for it. */
func parseQualityValue(value string) (float64, bool) {
    if "" == value {
        return 0, false
    }

    integerPart := value[0]
    if '0' != integerPart && '1' != integerPart {
        return 0, false
    }

    decimals := value[1:]
    if "" != decimals {
        if '.' != decimals[0] {
            return 0, false
        }

        decimals = decimals[1:]
        if 3 < len(decimals) {
            return 0, false
        }
    }

    qualityValue := 0.0
    if '1' == integerPart {
        qualityValue = 1.0
    }

    scale := 0.1
    for index := 0; index < len(decimals); index++ {
        digit := decimals[index]
        if digit < '0' || digit > '9' {
            return 0, false
        }

        if '1' == integerPart && '0' != digit {
            return 0, false
        }

        qualityValue += float64(digit-'0') * scale
        scale = scale / 10
    }

    return qualityValue, true
}

/* @important a member whose q parameter falls outside the RFC 7231 qvalue grammar is dropped whole rather than rounded to a guess: the previous leniency scored an unparseable q as full acceptance, clamped a negative one into a refusal and let NaN through as a weight that no comparison could select or refuse, so the same malformed header could open, close or silently poison the negotiation depending on the spelling */
func parseAcceptHeader(acceptHeader string) []acceptedMime {
    parts := splitOutsideQuotes(acceptHeader, ',')
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
                parameters := splitOutsideQuotes(parametersPart, ';')
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
                        parsedValue, valid := parseQualityValue(value)
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

/* @important a q of 0 is a refusal, not an absence: it is kept in the parsed list so a candidate it covers can be excluded rather than falling through to the default */
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
