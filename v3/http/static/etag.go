package static

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io/fs"
    "strings"

    "github.com/precision-soft/melody/v3/version"
)

/* GenerateEtag hashes the file size and nanosecond modification time into a 64-bit tag. When modification time is zero, it uses the build version instead, so embedded assets require a distinct build version to invalidate same-size replacements.

   This is a metadata-based validator, not a content hash: unchanged metadata and hash collisions can produce the same tag. The digest avoids spelling out timestamps or build versions in the header; it is not a secrecy guarantee. A nil FileInfo returns an empty tag. */
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

/* EtagMatchesIfNoneMatch performs weak comparison over comma-separated tags, ignoring W/ prefixes. It intentionally does not honor the wildcard form. */
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
