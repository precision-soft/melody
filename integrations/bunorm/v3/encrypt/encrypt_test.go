package encrypt_test

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

func newKey(filler byte) []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = filler
    }
    return key
}

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := encrypt.NewCipher(provider)

    encoded, encryptErr := cipher.Encrypt("top secret")
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if "top secret" == encoded {
        t.Fatalf("expected ciphertext to differ from plaintext")
    }

    if false == strings.HasPrefix(encoded, "<ENC>\x00gcm1\x00v1:") {
        t.Fatalf("expected marker + key id prefix, got %q", encoded)
    }

    decrypted, decryptErr := cipher.Decrypt(encoded)
    if nil != decryptErr {
        t.Fatalf("decrypt: %v", decryptErr)
    }

    if "top secret" != decrypted {
        t.Fatalf("round-trip mismatch: %q", decrypted)
    }
}

func TestCipher_DecryptFailsWithUnknownKeyId(t *testing.T) {
    encryptingProvider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    encoded, _ := encrypt.NewCipher(encryptingProvider).Encrypt("secret")

    decryptingProvider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    _, decryptErr := encrypt.NewCipher(decryptingProvider).Decrypt(encoded)
    if nil == decryptErr {
        t.Fatalf("expected decrypt to fail without the original key")
    }
}

func TestCipher_EncryptDeterministicWithKeyIdStaysSearchableAcrossRotation(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2), "v1": newKey(1)})
    cipher := encrypt.NewCipher(provider)

    rotated, rotateErr := cipher.EncryptDeterministicWithKeyId("alice@example.com", "v2")
    if nil != rotateErr {
        t.Fatalf("deterministic encrypt with key id: %v", rotateErr)
    }

    again, _ := cipher.EncryptDeterministicWithKeyId("alice@example.com", "v2")
    if rotated != again {
        t.Fatalf("expected deterministic ciphertext to be stable, got %q vs %q", rotated, again)
    }

    candidates, candidatesErr := cipher.CiphertextCandidates("alice@example.com")
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    matched := false
    for _, candidate := range candidates {
        if string(candidate) == rotated {
            matched = true
        }
    }
    if false == matched {
        t.Fatalf("rotated deterministic value %q not found among lookup candidates %v", rotated, candidates)
    }
}

func TestEncryptedString_ValueScanRoundTrip(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    encrypt.UseCipher(encrypt.NewCipher(provider))
    defer encrypt.UseCipher(nil)

    original := encrypt.EncryptedString("personal data")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes || "personal data" == string(storedBytes) {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded encrypt.EncryptedString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "personal data" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}

func TestEncryptedDeterministicString_ValuePreservesMarkerBytes(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    encrypt.UseCipher(encrypt.NewCipher(provider))
    defer encrypt.UseCipher(nil)

    original := encrypt.EncryptedDeterministicString("alice@example.com")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes {
        t.Fatalf("deterministic value must be []byte so bun emits a binary literal, got %T", stored)
    }

    if false == strings.Contains(string(storedBytes), "\x00") {
        t.Fatalf("expected the encryption marker nul bytes to survive in the stored value")
    }

    var loaded encrypt.EncryptedDeterministicString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "alice@example.com" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}

func TestCipher_DecryptPassesThroughLegacyPlaintext(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := encrypt.NewCipher(provider)

    decrypted, decryptErr := cipher.Decrypt("legacy plaintext value")
    if nil != decryptErr {
        t.Fatalf("decrypt of unmarked plaintext should pass through: %v", decryptErr)
    }

    if "legacy plaintext value" != decrypted {
        t.Fatalf("expected plaintext passthrough, got %q", decrypted)
    }
}

func TestCipher_EncryptIsIdempotentOnAlreadyEncrypted(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := encrypt.NewCipher(provider)

    once, _ := cipher.Encrypt("value")
    twice, encryptErr := cipher.Encrypt(once)
    if nil != encryptErr {
        t.Fatalf("re-encrypt: %v", encryptErr)
    }

    if once != twice {
        t.Fatalf("expected double-encryption guard to return the value unchanged")
    }
}

func TestCipher_DeterministicProducesStableCiphertext(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := encrypt.NewCipher(provider)

    first, _ := cipher.EncryptDeterministic("lookup@example.com")
    second, _ := cipher.EncryptDeterministic("lookup@example.com")
    if first != second {
        t.Fatalf("deterministic encryption must yield equal ciphertext for equal plaintext")
    }

    other, _ := cipher.EncryptDeterministic("different@example.com")
    if first == other {
        t.Fatalf("different plaintext must yield different ciphertext")
    }

    decrypted, decryptErr := cipher.Decrypt(first)
    if nil != decryptErr || "lookup@example.com" != decrypted {
        t.Fatalf("deterministic round-trip failed: %q (%v)", decrypted, decryptErr)
    }

    random, _ := cipher.Encrypt("lookup@example.com")
    if random == first {
        t.Fatalf("random and deterministic encodings should differ")
    }
}

func TestCipher_CiphertextCandidatesCoverAllActiveKeys(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := encrypt.NewCipher(provider)

    candidates, candidatesErr := cipher.CiphertextCandidates("user@example.com")
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    if 2 != len(candidates) {
        t.Fatalf("expected one candidate per active key, got %d", len(candidates))
    }

    current, _ := cipher.EncryptDeterministic("user@example.com")
    if string(candidates[0]) != current {
        t.Fatalf("expected the current key candidate first")
    }
}

func TestStaticKeyProvider_ActiveKeyIdsCurrentFirst(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2), "v3": newKey(3)})

    active := provider.ActiveKeyIds()
    if 3 != len(active) || "v2" != active[0] {
        t.Fatalf("expected current key first, got %v", active)
    }
}

func TestFakeCipher_IsIdentity(t *testing.T) {
    cipher := encrypt.NewFakeCipher()

    encoded, _ := cipher.Encrypt("value")
    if "value" != encoded {
        t.Fatalf("fake cipher must not transform the value")
    }

    decrypted, _ := cipher.Decrypt(encoded)
    if "value" != decrypted {
        t.Fatalf("fake cipher round-trip mismatch")
    }
}

func TestEncryptedString_FailsClosedWithoutCipher(t *testing.T) {
    encrypt.UseCipher(nil)

    secret := encrypt.EncryptedString("personal data")
    if _, valueErr := secret.Value(); nil == valueErr {
        t.Fatalf("expected Value to fail when no cipher is configured")
    }

    var loaded encrypt.EncryptedString
    if scanErr := loaded.Scan("anything"); nil == scanErr {
        t.Fatalf("expected Scan to fail when no cipher is configured")
    }

    if scanErr := loaded.Scan(nil); nil != scanErr {
        t.Fatalf("scan of NULL should succeed: %v", scanErr)
    }
}

func TestMigrator_EncryptThenReencryptRoundTripValues(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := encrypt.NewCipher(provider)

    encrypted, _ := cipher.EncryptWithKeyId("secret", "v1")
    plaintext, _ := cipher.Decrypt(encrypted)
    rotated, _ := cipher.EncryptWithKeyId(plaintext, "v2")

    if false == strings.HasPrefix(rotated, "<ENC>\x00gcm1\x00v2:") {
        t.Fatalf("expected rotated value under v2, got %q", rotated)
    }

    final, decryptErr := cipher.Decrypt(rotated)
    if nil != decryptErr || "secret" != final {
        t.Fatalf("rotation round-trip failed: %q (%v)", final, decryptErr)
    }
}

func TestEncryptedString_MasksPlaintextWhenFormatted(t *testing.T) {
    secret := encrypt.EncryptedString("personal data")

    if "personal data" == secret.String() {
        t.Fatalf("expected String to mask the plaintext")
    }

    for _, formatted := range []string{
        fmt.Sprintf("%v", secret),
        fmt.Sprintf("%s", secret),
        fmt.Sprintf("the value is %v", secret),
    } {
        if true == strings.Contains(formatted, "personal data") {
            t.Fatalf("expected formatted output to hide the plaintext, got %q", formatted)
        }
    }

    if "personal data" != string(secret) {
        t.Fatalf("explicit string conversion must still expose the value")
    }
}
