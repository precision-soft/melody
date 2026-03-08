package serializer

import (
    "sort"
    "strconv"
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

func parseAcceptHeader(acceptHeader string) []acceptedMime {
    parts := strings.Split(acceptHeader, ",")
    result := make([]acceptedMime, 0, len(parts))

    for _, part := range parts {
        part = strings.TrimSpace(part)
        if "" == part {
            continue
        }

        mimePart := part
        qualityValue := 1.0

        parameterSeparatorIndex := strings.Index(part, ";")
        if -1 != parameterSeparatorIndex {
            mimePart = strings.TrimSpace(part[:parameterSeparatorIndex])
            parametersPart := strings.TrimSpace(part[parameterSeparatorIndex+1:])

            if "" != parametersPart {
                parameters := strings.Split(parametersPart, ";")
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
                        parsedValue, err := strconv.ParseFloat(value, 64)
                        if nil == err {
                            if 0 > parsedValue {
                                parsedValue = 0
                            }
                            if 1 < parsedValue {
                                parsedValue = 1
                            }

                            qualityValue = parsedValue
                        }
                    }
                }
            }
        }

        mimePart = normalizeMime(mimePart)
        if "" == mimePart {
            continue
        }

        if 0 == qualityValue {
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
