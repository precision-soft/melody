package static

import (
    "fmt"
    "io/fs"
    "strings"

    "github.com/precision-soft/melody/version"
)

/* GenerateEtag derives the entity tag from the size and the modification time at nanosecond resolution, or from the size and the build version for a filesystem that carries no modification time. An embedded filesystem reports the zero instant for every file, so the build version stands in: every asset revalidates once after a deploy and none of the previous build is served stale. A filesystem that reports whole seconds keeps a whole-second tag. */
func GenerateEtag(info fs.FileInfo, weak bool) string {
    if nil == info {
        return ""
    }

    if true == info.ModTime().IsZero() {
        return formatEtag(fmt.Sprintf("%d-%s", info.Size(), version.BuildVersion()), weak)
    }

    return formatEtag(fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano()), weak)
}

func formatEtag(etag string, weak bool) string {
    if true == weak {
        return fmt.Sprintf("W/%q", etag)
    }

    return fmt.Sprintf("%q", etag)
}

/* EtagMatchesIfNoneMatch reports whether the If-None-Match header names the entity tag, reading the header as a comma-separated list under the RFC weak comparison, which ignores the W/ prefix on either side, since a proxy may weaken a strong tag. The wildcard form is not honoured, so an attacker-supplied header cannot force an unconditional 304. */
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
