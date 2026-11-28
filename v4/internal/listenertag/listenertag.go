package listenertag

import (
    "slices"
    "strings"
    "unicode"
)

/* FrameworkPrefix is the namespace of the tags the framework defines; no other tag may be in it */
const FrameworkPrefix = "melody."

const (
    FrameworkAccessLog = FrameworkPrefix + "access_log"
    FrameworkCors      = FrameworkPrefix + "cors"
    FrameworkSecurityHeaders = FrameworkPrefix + "security_headers"
)

const (
    RefusalEmpty      = "may not carry an empty tag"
    RefusalWhitespace = "carries a tag with whitespace"
    RefusalReserved   = "carries a tag in the melody. namespace the framework does not define"
    RefusalTwice      = "carries a tag twice"
)

/* FrameworkTags answers the tags the framework defines, a list of the caller's own */
func FrameworkTags() []string {
    return []string{FrameworkAccessLog, FrameworkCors, FrameworkSecurityHeaders}
}

/* Refused answers the first tag of the list a dispatch could not match exactly or that the framework's namespace does not hold: an empty tag, a tag carrying whitespace or a control character, a tag in the melody. namespace the framework does not define, or a tag listed twice; refused is false for a list that passes, nil and an empty list included. */
func Refused(tags []string) (reason string, index int, tag string, refused bool) {
    seenTags := make(map[string]struct{}, len(tags))

    for tagIndex, candidate := range tags {
        if "" == candidate {
            return RefusalEmpty, tagIndex, candidate, true
        }

        for _, character := range candidate {
            if true == unicode.IsSpace(character) || true == unicode.IsControl(character) {
                return RefusalWhitespace, tagIndex, candidate, true
            }
        }

        if true == strings.HasPrefix(candidate, FrameworkPrefix) && false == slices.Contains(FrameworkTags(), candidate) {
            return RefusalReserved, tagIndex, candidate, true
        }

        if _, seen := seenTags[candidate]; true == seen {
            return RefusalTwice, tagIndex, candidate, true
        }

        seenTags[candidate] = struct{}{}
    }

    return "", 0, "", false
}
