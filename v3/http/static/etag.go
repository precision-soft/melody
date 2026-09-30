package static

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io/fs"
    "strconv"
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

/* entityTag is the tag the server sends. A file that carries a modification time is tagged by GenerateEtag. One that carries none, every file of an embedded filesystem, is tagged by its size and a digest of its bytes: the build version GenerateEtag falls back to is melody's own, and it does not move when an application ships a changed asset of the same size, so a revalidating client would be answered 304 over stale bytes. The digest is kept per path, since embedded bytes are fixed for the life of the process; a file that cannot be read keeps GenerateEtag's tag. */
func (instance *FileServer) entityTag(relativePath string, fileInfo fs.FileInfo) string {
    if nil == fileInfo || false == fileInfo.ModTime().IsZero() {
        return GenerateEtag(fileInfo, instance.config.weakEtag)
    }

    cacheKey := relativePath + "\x00" + strconv.FormatInt(fileInfo.Size(), 10)
    if nil != instance.contentTags {
        if cached, found := instance.contentTags.Load(cacheKey); true == found {
            return cached.(string)
        }
    }

    content, readErr := fs.ReadFile(instance.fileSystem, relativePath)
    if nil != readErr {
        return GenerateEtag(fileInfo, instance.config.weakEtag)
    }

    digest := sha256.Sum256(content)
    etag := formatEtag(strconv.FormatInt(fileInfo.Size(), 10)+"-"+hex.EncodeToString(digest[:8]), instance.config.weakEtag)

    if nil != instance.contentTags {
        instance.contentTags.Store(cacheKey, etag)
    }

    return etag
}
