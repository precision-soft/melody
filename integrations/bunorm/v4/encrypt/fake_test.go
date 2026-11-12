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

/* the fake carries the binding door as the identity, so a bound column over it reads back what it stores */
func TestFakeCipher_ServesABoundColumnAsIdentity(t *testing.T) {
    installCipher(t, crmCipherRef{}.CipherName(), NewFakeCipher())
    installSealBinding(t, crmIbanBinding{}, true)

    stored, valueErr := EncryptedStringFor[crmIbanBinding]("value").Value()
    if nil != valueErr || "value" != string(stored.([]byte)) {
        t.Fatalf("expected the fake to store the plaintext, got %q (%v)", stored, valueErr)
    }

    var scanned EncryptedStringFor[crmIbanBinding]
    if scanErr := scanned.Scan(stored); nil != scanErr || "value" != string(scanned) {
        t.Fatalf("expected the fake to read it back, got %q (%v)", string(scanned), scanErr)
    }

    if candidates, candidatesErr := CiphertextCandidatesFor[crmIbanBinding]("value"); nil != candidatesErr || 1 != len(candidates) || "value" != string(candidates[0]) {
        t.Fatalf("expected the identity candidate, got %q (%v)", candidates, candidatesErr)
    }
}
