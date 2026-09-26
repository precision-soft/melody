package static

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io/fs"
    "strings"

    "github.com/precision-soft/melody/v3/version"
)

/* GenerateEtag derives the entity tag from the size and the modification time at nanosecond resolution, or from the size and the build version for a filesystem with no modification time, as an embedded one is. The published tag is a truncated sha256 of that derivation, so it discloses neither. */
func GenerateEtag(info fs.FileInfo, weak bool) string {
    if nil == info {
        return ""
    }

    if true == info.ModTime().IsZero() {
        return formatEtag(digestEtagInput(fmt.Sprintf("%d-%s", info.Size(), version.BuildVersion())), weak)
    }

    return formatEtag(digestEtagInput(fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano())), weak)
}

func digestEtagInput(input string) string {
    digest := sha256.Sum256([]byte(input))

    return hex.EncodeToString(digest[:8])
}

func formatEtag(etag string, weak bool) string {
    if true == weak {
        return fmt.Sprintf("W/%q", etag)
    }

    return fmt.Sprintf("%q", etag)
}

/* EtagMatchesIfNoneMatch applies the RFC weak comparison over the comma-separated list, ignoring W/ on either side. The wildcard form is deliberately not honoured, since it would make a client header an unconditional 304. */
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
