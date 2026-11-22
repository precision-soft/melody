package entitytag

import (
    "crypto/sha256"
    "encoding/hex"
    "strconv"
)

/* Digest answers the published form of an entity tag's derivation, unquoted: a truncated sha256, so the tag discloses neither the size nor the time it is derived from. */
func Digest(input string) string {
    digest := sha256.Sum256([]byte(input))

    return hex.EncodeToString(digest[:8])
}

/* OfSizeAndModificationTime is the tag of a file of this size modified at this instant, the one the static file server and the local storage both answer. */
func OfSizeAndModificationTime(size int64, modifiedAtUnixNano int64) string {
    return Digest(strconv.FormatInt(size, 10) + "-" + strconv.FormatInt(modifiedAtUnixNano, 10))
}
