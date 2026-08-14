package security

import "testing"

/* The known answer is what ties this function to the seeded accounts: the seed writes the hash of "user" into the catalogue and the login handler hashes what the form submitted, so a change of algorithm or encoding here locks every seeded account out with no other symptom than a refused password. */
func TestSha256HexAnswersTheKnownDigest(t *testing.T) {
    for value, expected := range map[string]string{
        "user":   "04f8996da763b7a969b1028ee3007569eaf3a635486ddab211d512c85b9df8fb",
        "editor": "1553cc62ff246044c683a61e203e65541990e7fcd4af9443d22b9557ecc9ac54",
        "admin":  "8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918",
        "":       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    } {
        if expected != Sha256Hex(value) {
            t.Fatalf("digest of %q: got %q, want %q", value, Sha256Hex(value), expected)
        }
    }
}

func TestSha256HexIsStable(t *testing.T) {
    if Sha256Hex("user") != Sha256Hex("user") {
        t.Fatalf("the digest of one value differs between two calls")
    }

    if Sha256Hex("user") == Sha256Hex("User") {
        t.Fatalf("the digest ignores case, so two distinct passwords collide")
    }
}
