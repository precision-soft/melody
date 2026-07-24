package static

import (
    "fmt"
    "io/fs"
    "strings"
)

func GenerateEtag(info fs.FileInfo, weak bool) string {
    if nil == info {
        return ""
    }

    etag := fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix())

    if true == weak {
        return fmt.Sprintf("W/%q", etag)
    }

    return fmt.Sprintf("%q", etag)
}

/* @important the header is a comma-separated list and a proxy may weaken a strong tag, so an exact string comparison silently re-sends the whole body; the RFC weak comparison ignores the W/ prefix on either side. The wildcard form is deliberately not honoured — it would turn an attacker-supplied header into an unconditional 304 for no practical gain. */
func EtagMatchesIfNoneMatch(ifNoneMatch string, etag string) bool {
    if "" == strings.TrimSpace(ifNoneMatch) || "" == etag {
        return false
    }

    normalizedEtag := strings.TrimPrefix(etag, "W/")

    for _, candidate := range strings.Split(ifNoneMatch, ",") {
        candidate = strings.TrimSpace(candidate)
        if "" == candidate {
            continue
        }

        if normalizedEtag == strings.TrimPrefix(candidate, "W/") {
            return true
        }
    }

    return false
}
