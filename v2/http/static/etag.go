package static

import (
    "fmt"
    "io/fs"
    "strings"

    "github.com/precision-soft/melody/v2/version"
)

/* GenerateEtag derives the entity tag from the size and the modification time, and from the size and the BUILD VERSION for a filesystem that carries no modification time. An embedded filesystem is that filesystem: every FileInfo it hands out reports the zero instant, so the size-and-time form degenerated into size alone — identical across every rebuild — and a redeployed asset that kept its length (a version string, a colour, a bundle that re-minified to the same size) revalidated 304 and stayed served stale for the life of the deployment. The build version is the coarser but honest stand-in: every asset revalidates once after a deploy and no asset of the previous build survives it.

The modification time is read at NANOSECOND resolution. At the whole second the Unix form used, two rewrites within the same second that kept the same length produced an identical tag — a deploy that swapped a bundle for one of the same size revalidated 304 and stayed served stale until its length or its second changed. If-None-Match carries this tag and takes precedence over If-Modified-Since, so the finer tag catches the change the second-resolution Last-Modified cannot. A filesystem whose FileInfo reports only whole seconds keeps the whole-second tag; nothing is lost. */
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

/* the header is a comma-separated list and a proxy may weaken a strong tag, so an exact string comparison silently re-sends the whole body; the RFC weak comparison ignores the W/ prefix on either side. The wildcard form is deliberately not honoured — it would turn an attacker-supplied header into an unconditional 304 for no practical gain. */
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
