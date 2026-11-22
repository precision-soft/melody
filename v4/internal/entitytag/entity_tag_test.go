package entitytag

import (
    "crypto/sha256"
    "encoding/hex"
    "testing"
)

/* the derivation is pinned whole: a tag that changes across a release revalidates every cached file and object, so the formula is a published value */
func TestOfSizeAndModificationTime_IsTheTruncatedDigestOfSizeAndNanoseconds(t *testing.T) {
    digest := sha256.Sum256([]byte("42-1700000000123456789"))
    expected := hex.EncodeToString(digest[:8])

    if expected != OfSizeAndModificationTime(42, 1700000000123456789) || 16 != len(expected) {
        t.Fatalf("expected %s, got %s", expected, OfSizeAndModificationTime(42, 1700000000123456789))
    }

    if OfSizeAndModificationTime(42, 1) == OfSizeAndModificationTime(43, 1) || OfSizeAndModificationTime(42, 1) == OfSizeAndModificationTime(42, 2) {
        t.Fatalf("expected the size and the time each to move the tag")
    }
}
