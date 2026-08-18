package security

import (
    "testing"
)

/* the digest is pinned against a published vector rather than against another call of the same function: a comparison with itself would hold under any hash, and the stored passwords of every seeded user are only readable by the exact algorithm that wrote them. */
func TestSha256HexAnswersThePublishedDigest(t *testing.T) {
    digest := Sha256Hex("abc")

    if "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" != digest {
        t.Fatalf("expected the sha-256 digest of %q, got %q", "abc", digest)
    }
}

func TestSha256HexAnswersTheEmptyInputDigest(t *testing.T) {
    digest := Sha256Hex("")

    if "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" != digest {
        t.Fatalf("expected the sha-256 digest of the empty string, got %q", digest)
    }
}

/* the hasher carries no salt, so two calls agree: the login door compares the digest of what was submitted against the digest stored at seeding, and a per-call salt would make every comparison fail. */
func TestSha256HexIsDeterministic(t *testing.T) {
    if Sha256Hex("melody") != Sha256Hex("melody") {
        t.Fatal("expected two digests of one password to agree")
    }

    if Sha256Hex("melody") == Sha256Hex("melodz") {
        t.Fatal("expected two different passwords to answer different digests")
    }
}
