package encrypt

import (
    "encoding/base64"
    "strings"
    "testing"
)

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

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
    encryptingProvider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    encoded, _ := NewCipher(encryptingProvider).Encrypt("secret")

    decryptingProvider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    _, decryptErr := NewCipher(decryptingProvider).Decrypt(encoded)
    if nil == decryptErr {
        t.Fatalf("expected decrypt to fail without the original key")
    }
}

func TestCipher_EncryptDeterministicWithKeyIdStaysSearchableAcrossRotation(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2), "v1": newKey(1)})
    cipher := NewCipher(provider)

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

func TestCipher_DecryptPassesThroughLegacyPlaintext(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    decrypted, decryptErr := cipher.Decrypt("legacy plaintext value")
    if nil != decryptErr {
        t.Fatalf("decrypt of unmarked plaintext should pass through: %v", decryptErr)
    }

    if "legacy plaintext value" != decrypted {
        t.Fatalf("expected plaintext passthrough, got %q", decrypted)
    }
}

func TestCipher_EncryptIsIdempotentOnAlreadyEncrypted(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    once, _ := cipher.Encrypt("value")
    twice, encryptErr := cipher.Encrypt(once)
    if nil != encryptErr {
        t.Fatalf("re-encrypt: %v", encryptErr)
    }

    if once != twice {
        t.Fatalf("expected double-encryption guard to return the value unchanged")
    }
}

/** @info marker-shaped plaintext */

func TestCipher_EncryptSealsMarkerShapedPlaintextInsteadOfStoringItRaw(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    forged := "<ENC>\x00gcm1\x00v1:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    if _, decryptErr := cipher.Decrypt(forged); nil == decryptErr {
        t.Fatalf("precondition: the forged value must not authenticate under the known key")
    }

    encoded, encryptErr := cipher.Encrypt(forged)
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if forged == encoded {
        t.Fatalf("expected the marker-shaped plaintext to be sealed; storing it raw poisons every later read")
    }

    decrypted, decryptErr := cipher.Decrypt(encoded)
    if nil != decryptErr {
        t.Fatalf("decrypt: %v", decryptErr)
    }

    if forged != decrypted {
        t.Fatalf("round-trip mismatch: %q", decrypted)
    }
}

func TestCipher_EncryptDeterministicSealsMarkerShapedPlaintext(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    forged := "<ENC>\x00gcm1\x00v1:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    encoded, encryptErr := cipher.EncryptDeterministic(forged)
    if nil != encryptErr {
        t.Fatalf("deterministic encrypt: %v", encryptErr)
    }

    if forged == encoded {
        t.Fatalf("expected the marker-shaped plaintext to be sealed deterministically")
    }

    candidates, candidatesErr := cipher.CiphertextCandidates(forged)
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    matched := false
    for _, candidate := range candidates {
        if string(candidate) == encoded {
            matched = true
        }
    }
    if false == matched {
        t.Fatalf("stored deterministic value %q not found among lookup candidates", encoded)
    }
}

func TestCipher_EncryptPassesThroughCiphertextSealedUnderRetiredKey(t *testing.T) {
    sealingProvider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    sealed, sealErr := NewCipher(sealingProvider).Encrypt("secret")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    rotatedProvider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    rotatedCipher := NewCipher(rotatedProvider)

    encoded, encryptErr := rotatedCipher.Encrypt(sealed)
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if sealed != encoded {
        t.Fatalf("expected a value sealed under a retired key to pass through unchanged, not be double-encrypted")
    }
}

func TestCipher_DeterministicProducesStableCiphertext(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

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
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)

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

func TestMigrator_EncryptThenReencryptRoundTripValues(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)

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

/** @info keyIdOf */

func TestKeyIdOf_ReportsKeyForEncryptedValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)

    encrypted, encryptErr := cipher.EncryptWithKeyId("secret", "v1")
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    keyId, encryptedFlag := keyIdOf(encrypted)
    if false == encryptedFlag || "v1" != keyId {
        t.Fatalf("expected key id v1, got %q (encrypted=%v)", keyId, encryptedFlag)
    }

    if _, plaintextFlag := keyIdOf("plain text"); true == plaintextFlag {
        t.Fatalf("expected plaintext to report not-encrypted")
    }
}

func TestReencryptSkipAvoidsNonceRewrite(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)

    underTarget, _ := cipher.EncryptWithKeyId("secret", "v2")

    plaintext, _ := cipher.Decrypt(underTarget)
    rewritten, _ := cipher.EncryptWithKeyId(plaintext, "v2")
    if underTarget == rewritten {
        t.Fatalf("expected a fresh nonce to change the ciphertext, proving the skip is needed")
    }

    keyId, _ := keyIdOf(underTarget)
    if "v2" != keyId {
        t.Fatalf("expected the value to already be under the target key, got %q", keyId)
    }
}
