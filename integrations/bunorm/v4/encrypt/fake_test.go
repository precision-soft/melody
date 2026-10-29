package encrypt

import (
    "testing"
)

func TestFakeCipher_IsIdentity(t *testing.T) {
    cipher := NewFakeCipher()

    encoded, _ := cipher.Encrypt("value")
    if "value" != encoded {
        t.Fatalf("fake cipher must not transform the value")
    }

    decrypted, _ := cipher.Decrypt(encoded)
    if "value" != decrypted {
        t.Fatalf("fake cipher round-trip mismatch")
    }
}
