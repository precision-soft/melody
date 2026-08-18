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

    /* the retired key v1 stays in the set (still decryptable) until re-encryption completes; only then is it removed */
    rotatedProvider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2), "v1": newKey(1)})
    rotatedCipher := NewCipher(rotatedProvider)

    encoded, encryptErr := rotatedCipher.Encrypt(sealed)
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if sealed != encoded {
        t.Fatalf("expected a value sealed under a retired key still in the set to pass through unchanged, not be double-encrypted")
    }
}

func TestCipher_EncryptSealsMarkerShapedPlaintextWithUnknownKeyId(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    /* a client-supplied marker-shaped string naming a key id that is not in the set must not be stored verbatim, or every later Scan/Decrypt of the row fails with key-not-found */
    forged := "<ENC>\x00gcm1\x00rogue:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    if _, decryptErr := cipher.Decrypt(forged); nil == decryptErr {
        t.Fatalf("precondition: the unknown-key forged value must not authenticate")
    }

    encoded, encryptErr := cipher.Encrypt(forged)
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if forged == encoded {
        t.Fatalf("expected the unknown-key marker-shaped plaintext to be sealed under the current key, not stored verbatim")
    }

    decrypted, decryptErr := cipher.Decrypt(encoded)
    if nil != decryptErr {
        t.Fatalf("decrypt: %v", decryptErr)
    }

    if forged != decrypted {
        t.Fatalf("round-trip mismatch: %q", decrypted)
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

func TestKeyIdOf_ReportsKeyForEncryptedValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)

    encrypted, encryptErr := cipher.EncryptWithKeyId("secret", "v1")
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    keyId, encryptedFlag, keyIdErr := keyIdOf(encrypted)
    if nil != keyIdErr {
        t.Fatalf("key id of an encrypted value: %v", keyIdErr)
    }

    if false == encryptedFlag || "v1" != keyId {
        t.Fatalf("expected key id v1, got %q (encrypted=%v)", keyId, encryptedFlag)
    }

    if _, plaintextFlag, plaintextErr := keyIdOf("plain text"); true == plaintextFlag || nil != plaintextErr {
        t.Fatalf("expected plaintext to report not-encrypted with no error, got %v", plaintextErr)
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

    keyId, _, _ := keyIdOf(underTarget)
    if "v2" != keyId {
        t.Fatalf("expected the value to already be under the target key, got %q", keyId)
    }
}

/* truncateSealed cuts a sealed value the way a column too narrow for it does under a non-strict sql_mode: the marker and the key id survive, the base64 payload keeps only its first characters. The retained payload length is chosen so the remainder no longer decodes to a whole sealed body — which is exactly the shape that used to be mistaken for plaintext. */
func truncateSealed(t *testing.T, sealed string, retainedPayloadCharacters int) string {
    t.Helper()

    if false == strings.HasPrefix(sealed, markerPrefix) {
        t.Fatalf("expected a sealed value carrying the marker, got %q", sealed)
    }

    body := sealed[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator {
        t.Fatalf("expected a key id separator in %q", sealed)
    }

    payload := body[separator+1:]
    if len(payload) <= retainedPayloadCharacters {
        t.Fatalf("expected the sealed payload to be longer than %d characters", retainedPayloadCharacters)
    }

    return sealed[:len(markerPrefix)+separator+1] + payload[:retainedPayloadCharacters]
}

/* a value that carries the framework's marker was written by this cipher, so a payload that no longer decodes is damage — a column truncated under sql_mode='' — and must be reported. Returning it verbatim with a nil error is how a truncated ciphertext used to read back as garbage that the application then stored on. */
func TestCipher_DecryptReportsATruncatedCiphertextInsteadOfPassingItThrough(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    sealed, encryptErr := cipher.Encrypt("alice@example.com")
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    /* 8 base64 characters decode cleanly to 6 bytes, which is short of the nonce; 5 do not decode at all */
    for _, retained := range []int{8, 5, 0} {
        truncated := truncateSealed(t, sealed, retained)

        plaintext, decryptErr := cipher.Decrypt(truncated)
        if nil == decryptErr {
            t.Fatalf("expected an error for a payload truncated to %d characters, got %q", retained, plaintext)
        }

        if "" != plaintext {
            t.Fatalf("expected no plaintext for a payload truncated to %d characters, got %q", retained, plaintext)
        }
    }
}

/* the marker with no key id separator behind it cannot name a key, so it is damage too. */
func TestCipher_DecryptReportsAMarkerWithNoKeyId(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    for _, damaged := range []string{markerPrefix, markerPrefix + "v1", markerPrefix + ":" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))} {
        if _, decryptErr := cipher.Decrypt(damaged); nil == decryptErr {
            t.Fatalf("expected an error for %q", damaged)
        }
    }
}

/* the pass-through for values carrying NO marker is what makes an incremental migration work: the rows not yet sealed keep reading while the column is converted one write at a time. */
func TestCipher_DecryptStillPassesGenuinePlaintextThrough(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    for _, plaintext := range []string{"", "alice@example.com", "<ENC>", "<ENC>\x00gcm2\x00v1:zzz", "not encrypted at all"} {
        decrypted, decryptErr := cipher.Decrypt(plaintext)
        if nil != decryptErr {
            t.Fatalf("expected %q to pass through, got %v", plaintext, decryptErr)
        }

        if plaintext != decrypted {
            t.Fatalf("expected %q to pass through unchanged, got %q", plaintext, decrypted)
        }
    }
}

/* keyIdOf classifies rows for the bulk migrator, so it must not report a truncated ciphertext as plaintext: doing so seals the wreckage a second time and reports the row as migrated. */
func TestKeyIdOf_ReportsATruncatedCiphertextAsAnError(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    sealed, _ := cipher.Encrypt("alice@example.com")
    truncated := truncateSealed(t, sealed, 8)

    keyId, encrypted, keyIdErr := keyIdOf(truncated)
    if nil == keyIdErr {
        t.Fatalf("expected an error for a truncated ciphertext, got %q (encrypted=%v)", keyId, encrypted)
    }

    if true == encrypted {
        t.Fatal("expected a truncated ciphertext not to be reported as a usable sealed value")
    }
}

/* an application still holding a marker-shaped string must not have it stored verbatim: the write side seals it, so it can never poison a later read. */
func TestCipher_EncryptSealsAMarkerShapedPlaintextThatDoesNotDecode(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})
    cipher := NewCipher(provider)

    sealed, _ := cipher.Encrypt("alice@example.com")
    truncated := truncateSealed(t, sealed, 8)

    resealed, encryptErr := cipher.Encrypt(truncated)
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if truncated == resealed {
        t.Fatal("expected a marker-shaped value that does not decode to be sealed rather than stored verbatim")
    }

    roundTripped, decryptErr := cipher.Decrypt(resealed)
    if nil != decryptErr {
        t.Fatalf("decrypt: %v", decryptErr)
    }

    if truncated != roundTripped {
        t.Fatalf("expected the sealed value to round-trip, got %q", roundTripped)
    }
}
