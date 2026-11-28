package encrypt

import (
    "bytes"
    "encoding/base64"
    "reflect"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    encoded, encryptErr := cipher.Encrypt("top secret", EncryptOptions{})
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
    encryptingProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    encoded, _ := NewCipher(encryptingProvider).Encrypt("secret", EncryptOptions{})

    decryptingProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2)}})
    _, decryptErr := NewCipher(decryptingProvider).Decrypt(encoded)
    if nil == decryptErr {
        t.Fatalf("expected decrypt to fail without the original key")
    }
}

func TestCipher_EncryptDeterministicUnderANamedKeyStaysSearchableAcrossRotation(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2), "v1": newKey(1)}})
    cipher := NewCipher(provider)

    rotated, rotateErr := cipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v2"})
    if nil != rotateErr {
        t.Fatalf("deterministic encrypt with key id: %v", rotateErr)
    }

    again, _ := cipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v2"})
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
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
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
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    once, _ := cipher.Encrypt("value", EncryptOptions{})
    twice, encryptErr := cipher.Encrypt(once, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("re-encrypt: %v", encryptErr)
    }

    if once != twice {
        t.Fatalf("expected double-encryption guard to return the value unchanged")
    }
}

func TestCipher_EncryptSealsMarkerShapedPlaintextInsteadOfStoringItRaw(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    forged := "<ENC>\x00gcm1\x00v1:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    if _, decryptErr := cipher.Decrypt(forged); nil == decryptErr {
        t.Fatalf("precondition: the forged value must not authenticate under the known key")
    }

    encoded, encryptErr := cipher.Encrypt(forged, EncryptOptions{})
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
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    forged := "<ENC>\x00gcm1\x00v1:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    encoded, encryptErr := cipher.EncryptDeterministic(forged, EncryptOptions{})
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
    sealingProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    sealed, sealErr := NewCipher(sealingProvider).Encrypt("secret", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    rotatedProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2), "v1": newKey(1)}})
    rotatedCipher := NewCipher(rotatedProvider)

    encoded, encryptErr := rotatedCipher.Encrypt(sealed, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if sealed != encoded {
        t.Fatalf("expected a value sealed under a retired key still in the set to pass through unchanged, not be double-encrypted")
    }
}

func TestCipher_EncryptSealsMarkerShapedPlaintextWithUnknownKeyId(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    forged := "<ENC>\x00gcm1\x00rogue:" + base64.RawStdEncoding.EncodeToString([]byte("forged payload bytes"))

    if _, decryptErr := cipher.Decrypt(forged); nil == decryptErr {
        t.Fatalf("precondition: the unknown-key forged value must not authenticate")
    }

    encoded, encryptErr := cipher.Encrypt(forged, EncryptOptions{})
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
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    first, _ := cipher.EncryptDeterministic("lookup@example.com", EncryptOptions{})
    second, _ := cipher.EncryptDeterministic("lookup@example.com", EncryptOptions{})
    if first != second {
        t.Fatalf("deterministic encryption must yield equal ciphertext for equal plaintext")
    }

    other, _ := cipher.EncryptDeterministic("different@example.com", EncryptOptions{})
    if first == other {
        t.Fatalf("different plaintext must yield different ciphertext")
    }

    decrypted, decryptErr := cipher.Decrypt(first)
    if nil != decryptErr || "lookup@example.com" != decrypted {
        t.Fatalf("deterministic round-trip failed: %q (%v)", decrypted, decryptErr)
    }

    random, _ := cipher.Encrypt("lookup@example.com", EncryptOptions{})
    if random == first {
        t.Fatalf("random and deterministic encodings should differ")
    }
}

func TestCipher_CiphertextCandidatesCoverAllActiveKeys(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)

    candidates, candidatesErr := cipher.CiphertextCandidates("user@example.com")
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    if 2 != len(candidates) {
        t.Fatalf("expected one candidate per active key, got %d", len(candidates))
    }

    current, _ := cipher.EncryptDeterministic("user@example.com", EncryptOptions{})
    if string(candidates[0]) != current {
        t.Fatalf("expected the current key candidate first")
    }
}

func TestMigrator_EncryptThenReencryptRoundTripValues(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)

    encrypted, _ := cipher.Encrypt("secret", EncryptOptions{KeyId: "v1"})
    plaintext, _ := cipher.Decrypt(encrypted)
    rotated, _ := cipher.Encrypt(plaintext, EncryptOptions{KeyId: "v2"})

    if false == strings.HasPrefix(rotated, "<ENC>\x00gcm1\x00v2:") {
        t.Fatalf("expected rotated value under v2, got %q", rotated)
    }

    final, decryptErr := cipher.Decrypt(rotated)
    if nil != decryptErr || "secret" != final {
        t.Fatalf("rotation round-trip failed: %q (%v)", final, decryptErr)
    }
}

func TestKeyIdOf_ReportsKeyForEncryptedValue(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)

    encrypted, encryptErr := cipher.Encrypt("secret", EncryptOptions{KeyId: "v1"})
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
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)

    underTarget, _ := cipher.Encrypt("secret", EncryptOptions{KeyId: "v2"})

    plaintext, _ := cipher.Decrypt(underTarget)
    rewritten, _ := cipher.Encrypt(plaintext, EncryptOptions{KeyId: "v2"})
    if underTarget == rewritten {
        t.Fatalf("expected a fresh nonce to change the ciphertext, proving the skip is needed")
    }

    keyId, _, _ := keyIdOf(underTarget)
    if "v2" != keyId {
        t.Fatalf("expected the value to already be under the target key, got %q", keyId)
    }
}

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

func TestCipher_DecryptReportsATruncatedCiphertextInsteadOfPassingItThrough(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    sealed, encryptErr := cipher.Encrypt("alice@example.com", EncryptOptions{})
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

func TestCipher_DecryptReportsAMarkerWithNoKeyId(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    for _, damaged := range []string{markerPrefix, markerPrefix + "v1", markerPrefix + ":" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))} {
        if _, decryptErr := cipher.Decrypt(damaged); nil == decryptErr {
            t.Fatalf("expected an error for %q", damaged)
        }
    }
}

func TestCipher_DecryptStillPassesGenuinePlaintextThrough(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    for _, plaintext := range []string{"", "alice@example.com", "<ENC>", "<ENC>gcm1v1:zzz", "not encrypted at all"} {
        decrypted, decryptErr := cipher.Decrypt(plaintext)
        if nil != decryptErr {
            t.Fatalf("expected %q to pass through, got %v", plaintext, decryptErr)
        }

        if plaintext != decrypted {
            t.Fatalf("expected %q to pass through unchanged, got %q", plaintext, decrypted)
        }
    }
}

func TestCipher_DecryptRefusesAnUnknownSealFormat(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    body := "c2VhbGVkLXBheWxvYWQtb2YtZW5vdWdoLWxlbmd0aA"
    _, decryptErr := cipherInstance.Decrypt("<ENC>\x00gcm9\x00v1:" + body)
    if nil == decryptErr || false == exception.ChainIs(decryptErr, ErrSealFormatUnknown) {
        t.Fatalf("expected ErrSealFormatUnknown, got %v", decryptErr)
    }

    if context := exception.FromError(decryptErr).Context(); 1 != len(context) || "gcm9" != context["format"] {
        t.Fatalf("expected the context to name the format alone, got %v", context)
    }

    sixteen := strings.Repeat("a", 16)
    if _, longestErr := cipherInstance.Decrypt("<ENC>\x00" + sixteen + "\x00v1:" + body); sixteen != exception.FromError(longestErr).Context()["format"] {
        t.Fatalf("expected a 16-character format named, got %v", longestErr)
    }

    if true == strings.Contains(decryptErr.Error(), "v1") || true == strings.Contains(decryptErr.Error(), body) {
        t.Fatalf("expected the refusal to carry neither the key id nor the body, got %q", decryptErr.Error())
    }
}

func TestCipher_DecryptRefusesAMarkerFamilyValueOfNoFormat(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    for _, stored := range []string{"<ENC>\x00", "<ENC>\x00zzz", "<ENC>\x00\x00", "<ENC>\x00GCM1\x00v1:x", "<ENC>\x00" + strings.Repeat("a", 17) + "\x00v1:x"} {
        _, decryptErr := cipherInstance.Decrypt(stored)
        if nil == decryptErr || false == exception.ChainIs(decryptErr, ErrSealFormatUnknown) {
            t.Fatalf("expected %q refused with ErrSealFormatUnknown, got %v", stored, decryptErr)
        }

        if _, named := exception.FromError(decryptErr).Context()["format"]; true == named {
            t.Fatalf("expected %q refused without a format, got %v", stored, exception.FromError(decryptErr).Context())
        }
    }
}

func TestCipher_EncryptSealsAnUnknownFormatValueAsData(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    installCipher(t, defaultCipherName, cipherInstance)

    applicationValue := "<ENC>\x00gcm9\x00v1:zzz"

    sealed, encryptErr := cipherInstance.Encrypt(applicationValue, EncryptOptions{})
    if nil != encryptErr || true == strings.HasPrefix(sealed, "<ENC>\x00gcm9") {
        t.Fatalf("expected the value sealed as data, got %q (%v)", sealed, encryptErr)
    }

    if decrypted, decryptErr := cipherInstance.Decrypt(sealed); nil != decryptErr || applicationValue != decrypted {
        t.Fatalf("expected the value back byte for byte, got %q (%v)", decrypted, decryptErr)
    }

    stored, valueErr := EncryptedString(applicationValue).Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    var scanned EncryptedString
    if scanErr := scanned.Scan(stored); nil != scanErr || applicationValue != string(scanned) {
        t.Fatalf("expected the column to read the value back, got %q (%v)", string(scanned), scanErr)
    }
}

func TestKeyIdOf_ReportsAnUnknownSealFormatAsAnError(t *testing.T) {
    keyId, encrypted, keyIdErr := keyIdOf("<ENC>\x00gcm9\x00v1:zzz")
    if nil == keyIdErr || false == exception.ChainIs(keyIdErr, ErrSealFormatUnknown) || true == encrypted || "" != keyId {
        t.Fatalf("expected ErrSealFormatUnknown, got %q %v %v", keyId, encrypted, keyIdErr)
    }
}

func TestEncryptedString_AMarkerFamilyValueThroughAWrappedCipherIsRefused(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(7)}}))
    installCipher(t, defaultCipherName, wrappedCipher{Cipher: packageCipher})

    if stored, valueErr := EncryptedString("<ENC>\x00gcm9\x00v1:zzz").Value(); nil == valueErr || false == strings.Contains(valueErr.Error(), "encryption marker") {
        t.Fatalf("expected the foreign-cipher refusal of the marker-family value, stored %q (%v)", stored, valueErr)
    }
}

func TestKeyIdOf_ReportsATruncatedCiphertextAsAnError(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    sealed, _ := cipher.Encrypt("alice@example.com", EncryptOptions{})
    truncated := truncateSealed(t, sealed, 8)

    keyId, encrypted, keyIdErr := keyIdOf(truncated)
    if nil == keyIdErr {
        t.Fatalf("expected an error for a truncated ciphertext, got %q (encrypted=%v)", keyId, encrypted)
    }

    if true == encrypted {
        t.Fatal("expected a truncated ciphertext not to be reported as a usable sealed value")
    }
}

func TestCipher_EncryptSealsAMarkerShapedPlaintextThatDoesNotDecode(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    sealed, _ := cipher.Encrypt("alice@example.com", EncryptOptions{})
    truncated := truncateSealed(t, sealed, 8)

    resealed, encryptErr := cipher.Encrypt(truncated, EncryptOptions{})
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

type invalidIdKeyProvider struct {
    keyId string
}

func (instance *invalidIdKeyProvider) CurrentKeyId() string {
    return instance.keyId
}

func (instance *invalidIdKeyProvider) ActiveKeyIds() []string {
    return []string{instance.keyId}
}

func (instance *invalidIdKeyProvider) Key(keyId string) ([]byte, error) {
    return newKey(1), nil
}

func TestCipher_SealRefusesAKeyIdTheWireFormatCannotCarry(t *testing.T) {
    for _, keyId := range []string{"", "v1:extra", "id with spaces"} {
        cipher := NewCipher(&invalidIdKeyProvider{keyId: keyId})

        _, encryptErr := cipher.Encrypt("plaintext", EncryptOptions{})
        if nil == encryptErr {
            t.Fatalf("expected the key id %q to be refused at seal", keyId)
        }

        if false == strings.Contains(encryptErr.Error(), "key id must match") {
            t.Fatalf("expected the refusal to name the grammar, got: %v", encryptErr)
        }
    }
}

func TestCipher_DecryptRefusesANonCanonicalBase64Spelling(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    sealed, sealErr := cipher.Encrypt("", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    lastCharacter := sealed[len(sealed)-1]
    neighbour, known := map[byte]string{'A': "B", 'Q': "R", 'g': "h", 'w': "x"}[lastCharacter]
    if false == known {
        t.Fatalf("expected a canonical final quantum character, got %q", lastCharacter)
    }

    tampered := sealed[:len(sealed)-1] + neighbour

    if plaintext, decryptErr := cipher.Decrypt(tampered); nil == decryptErr {
        t.Fatalf("expected the non-canonical spelling to be refused, decrypted to %q", plaintext)
    }
}

func TestCipher_DecryptReportsAStructurallyShortPayloadAsDamage(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})
    cipher := NewCipher(provider)

    shortPayload := markerPrefix + "v1:" + base64.RawStdEncoding.EncodeToString(make([]byte, 20))

    _, decryptErr := cipher.Decrypt(shortPayload)
    if nil == decryptErr {
        t.Fatalf("expected the short payload to be refused")
    }

    if false == strings.Contains(decryptErr.Error(), "too short") {
        t.Fatalf("expected the structural class, got: %v", decryptErr)
    }
}

func TestCipher_EncryptDeterministicConvertsARandomSealInPlace(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    sealed, sealErr := cipher.Encrypt("alice", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    stored, encryptErr := cipher.EncryptDeterministic(sealed, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("deterministic encrypt: %v", encryptErr)
    }

    if sealed == stored {
        t.Fatalf("expected the random-nonce seal to be re-sealed deterministically, not passed through")
    }

    if false == deterministicCandidateMatches(t, cipher, "alice", stored) {
        t.Fatalf("expected the converted value to be found by the equality lookup")
    }

    decrypted, decryptErr := cipher.Decrypt(stored)
    if nil != decryptErr || "alice" != decrypted {
        t.Fatalf("expected the converted value to keep its plaintext, got %q (%v)", decrypted, decryptErr)
    }
}

func TestCipher_EncryptDeterministicKeepsTheKeyARandomSealCarries(t *testing.T) {
    retiredCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    sealedUnderRetired, sealErr := retiredCipher.Encrypt("alice", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    rotated := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2), "v1": newKey(1)}}))

    stored, encryptErr := rotated.EncryptDeterministic(sealedUnderRetired, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("deterministic encrypt: %v", encryptErr)
    }

    keyId, _, keyIdErr := keyIdOf(stored)
    if nil != keyIdErr || "v1" != keyId {
        t.Fatalf("expected the conversion to keep the key id the seal carried, got %q (%v)", keyId, keyIdErr)
    }

    expected, expectedErr := retiredCipher.EncryptDeterministic("alice", EncryptOptions{})
    if nil != expectedErr || expected != stored {
        t.Fatalf("expected the deterministic seal under the carried key, got another value (%v)", expectedErr)
    }
}

func TestCipher_EncryptDeterministicUnderANamedKeyKeepsTheKeyARandomSealCarries(t *testing.T) {
    retiredCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    sealedUnderRetired, sealErr := retiredCipher.Encrypt("alice", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    rotated := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2), "v1": newKey(1)}}))

    stored, encryptErr := rotated.EncryptDeterministic(sealedUnderRetired, EncryptOptions{KeyId: "v2"})
    if nil != encryptErr {
        t.Fatalf("deterministic encrypt: %v", encryptErr)
    }

    keyId, _, keyIdErr := keyIdOf(stored)
    if nil != keyIdErr || "v1" != keyId {
        t.Fatalf("expected the conversion to keep the key id the seal carried rather than rotate to the door's, got %q (%v)", keyId, keyIdErr)
    }

    if false == deterministicCandidateMatches(t, rotated, "alice", stored) {
        t.Fatalf("expected the converted value to be found by the equality lookup")
    }
}

func TestCipher_EncryptDeterministicPassesThroughADeterministicSealUnderARetiredKey(t *testing.T) {
    retiredCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    sealedUnderRetired, sealErr := retiredCipher.EncryptDeterministic("alice", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    rotated := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2), "v1": newKey(1)}}))

    stored, encryptErr := rotated.EncryptDeterministic(sealedUnderRetired, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("deterministic encrypt: %v", encryptErr)
    }

    if sealedUnderRetired != stored {
        t.Fatalf("expected a deterministic seal under a retired key still in the set to pass through unchanged")
    }
}

func TestCipher_EncryptPassesThroughADeterministicSeal(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    deterministic, sealErr := cipher.EncryptDeterministic("alice", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    stored, encryptErr := cipher.Encrypt(deterministic, EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    if deterministic != stored {
        t.Fatalf("expected the random door to pass a deterministic seal through unchanged")
    }
}

type rotatingKeyProvider struct {
    mutex sync.Mutex
    key   []byte
}

func (instance *rotatingKeyProvider) CurrentKeyId() string {
    return "v1"
}

func (instance *rotatingKeyProvider) ActiveKeyIds() []string {
    return []string{"v1"}
}

func (instance *rotatingKeyProvider) Key(keyId string) ([]byte, error) {
    if "v1" != keyId {
        return nil, exception.NewError("unknown key id", map[string]any{"keyId": keyId}, nil)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.key, nil
}

func (instance *rotatingKeyProvider) rotate(key []byte) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.key = key
}

func TestCipher_SealsUnderTheRotatedKeyWhenTheProviderChangesTheBytesUnderOneId(t *testing.T) {
    provider := &rotatingKeyProvider{key: newKey(1)}
    cipherInstance := NewCipher(provider)

    if _, warmErr := cipherInstance.Encrypt("secret", EncryptOptions{}); nil != warmErr {
        t.Fatalf("warm-up encrypt: %v", warmErr)
    }

    provider.rotate(newKey(2))

    sealedAfterRotation, encryptErr := cipherInstance.Encrypt("secret", EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("encrypt after rotation: %v", encryptErr)
    }

    rotatedReader := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(2)}}))

    decrypted, decryptErr := rotatedReader.Decrypt(sealedAfterRotation)
    if nil != decryptErr {
        t.Fatalf("expected the seal to be written under the rotated key, it does not open under it: %v", decryptErr)
    }

    if "secret" != decrypted {
        t.Fatalf("round-trip mismatch under the rotated key: %q", decrypted)
    }

    retiredReader := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    if _, retiredErr := retiredReader.Decrypt(sealedAfterRotation); nil == retiredErr {
        t.Fatalf("expected the seal not to open under the key the provider retired")
    }
}

func TestCipher_OpensUnderTheRotatedKeyWhenTheProviderChangesTheBytesUnderOneId(t *testing.T) {
    sealedUnderTheRotatedKey, encryptErr := NewCipher(
        NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(2)}}),
    ).Encrypt("secret", EncryptOptions{})
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    provider := &rotatingKeyProvider{key: newKey(1)}
    cipherInstance := NewCipher(provider)

    if _, warmErr := cipherInstance.Encrypt("warm the memo", EncryptOptions{}); nil != warmErr {
        t.Fatalf("warm-up encrypt: %v", warmErr)
    }

    provider.rotate(newKey(2))

    decrypted, decryptErr := cipherInstance.Decrypt(sealedUnderTheRotatedKey)
    if nil != decryptErr {
        t.Fatalf("expected the rotated key to be read, the memo answered the retired one: %v", decryptErr)
    }

    if "secret" != decrypted {
        t.Fatalf("round-trip mismatch under the rotated key: %q", decrypted)
    }
}

func TestCipher_ConcurrentSealsAndOpensShareTheKeyMaterial(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "v2",
        Keys: map[string][]byte{
            "v1": newKey(1),
            "v2": newKey(2),
            "v3": newKey(3),
        },
    }))

    const workers = 8

    start := make(chan struct{})
    failures := make(chan error, workers)

    var group sync.WaitGroup
    for worker := 0; worker < workers; worker++ {
        group.Add(1)

        go func(worker int) {
            defer group.Done()

            <-start

            for round := 0; round < 25; round++ {
                sealed, sealErr := cipherInstance.Encrypt("secret", EncryptOptions{KeyId: []string{"v1", "v2", "v3"}[worker%3]})
                if nil != sealErr {
                    failures <- sealErr

                    return
                }

                if _, candidatesErr := cipherInstance.CiphertextCandidates("secret"); nil != candidatesErr {
                    failures <- candidatesErr

                    return
                }

                opened, openErr := cipherInstance.Decrypt(sealed)
                if nil != openErr {
                    failures <- openErr

                    return
                }

                if "secret" != opened {
                    failures <- exception.NewError("round-trip mismatch", map[string]any{"value": opened}, nil)

                    return
                }
            }
        }(worker)
    }

    close(start)
    group.Wait()
    close(failures)

    for failure := range failures {
        t.Fatalf("concurrent round-trip: %v", failure)
    }
}

func BenchmarkCipher_Decrypt(b *testing.B) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    encoded, encryptErr := cipherInstance.Encrypt("a value of the size a column holds", EncryptOptions{})
    if nil != encryptErr {
        b.Fatalf("encrypt: %v", encryptErr)
    }

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        if _, decryptErr := cipherInstance.Decrypt(encoded); nil != decryptErr {
            b.Fatalf("decrypt: %v", decryptErr)
        }
    }
}

func BenchmarkCipher_EncryptDeterministic(b *testing.B) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        if _, encryptErr := cipherInstance.EncryptDeterministic("a value of the size a column holds", EncryptOptions{}); nil != encryptErr {
            b.Fatalf("encrypt: %v", encryptErr)
        }
    }
}

func BenchmarkCipher_EncryptDeterministicConvertsARandomNonceSeal(b *testing.B) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    randomNonceSeal, encryptErr := cipherInstance.Encrypt("a value of the size a column holds", EncryptOptions{})
    if nil != encryptErr {
        b.Fatalf("encrypt: %v", encryptErr)
    }

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        if _, convertErr := cipherInstance.EncryptDeterministic(randomNonceSeal, EncryptOptions{}); nil != convertErr {
            b.Fatalf("convert: %v", convertErr)
        }
    }
}

func BenchmarkCipher_CiphertextCandidates(b *testing.B) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "v3",
        Keys: map[string][]byte{
            "v1": newKey(1),
            "v2": newKey(2),
            "v3": newKey(3),
        },
    }))

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        if _, candidatesErr := cipherInstance.CiphertextCandidates("a value of the size a column holds"); nil != candidatesErr {
            b.Fatalf("candidates: %v", candidatesErr)
        }
    }
}

func BenchmarkGcmForKey(b *testing.B) {
    key := newKey(1)

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        if _, gcmErr := gcmForKey(key, "v1"); nil != gcmErr {
            b.Fatalf("gcm: %v", gcmErr)
        }
    }
}

func BenchmarkNonceSubKey(b *testing.B) {
    key := newKey(1)

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        nonceSubKey(key, deterministicNonceLabel)
    }
}

func BenchmarkDeterministicNonceFrom(b *testing.B) {
    nonceKey := nonceSubKey(newKey(1), deterministicNonceLabel)

    b.ReportAllocs()
    b.ResetTimer()

    for iteration := 0; iteration < b.N; iteration++ {
        deterministicNonceFrom(nonceKey, "a value of the size a column holds", minNonceSize)
    }
}

func TestEncryptedString_ASealWrittenAsDataThroughAWrappedCipherIsRefused(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(7)}}))
    installCipher(t, defaultCipherName, wrappedCipher{Cipher: packageCipher})

    otherRowsSeal, sealErr := packageCipher.Encrypt("another account's iban", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    stored, valueErr := EncryptedString(otherRowsSeal).Value()
    if nil == valueErr {
        t.Fatalf("expected the marker-shaped value refused through a wrapped cipher, stored %q", stored)
    }

    if true == strings.Contains(valueErr.Error(), "another account") || false == strings.Contains(valueErr.Error(), "encryption marker") {
        t.Fatalf("expected the foreign-cipher refusal without the value, got: %v", valueErr)
    }
}

func TestEncryptedDeterministicString_ASealWrittenAsDataThroughAWrappedCipherIsRefused(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(7)}}))
    installCipher(t, defaultCipherName, wrappedCipher{Cipher: packageCipher})

    otherRowsSeal, sealErr := packageCipher.EncryptDeterministic("another account's iban", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if stored, valueErr := EncryptedDeterministicString(otherRowsSeal).Value(); nil == valueErr {
        t.Fatalf("expected the marker-shaped value refused through a wrapped cipher, stored %q", stored)
    }
}

func TestEncryptedStringFor_ASealWrittenAsDataThroughAWrappedNamedCipherIsRefused(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v1", Keys: map[string][]byte{"crm-v1": newKey(11)}}))
    installCipher(t, crmCipherReference{}.CipherName(), wrappedCipher{Cipher: packageCipher})

    otherRowsSeal, sealErr := packageCipher.Encrypt("another customer's iban", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if stored, valueErr := EncryptedStringFor[crmCipherReference](otherRowsSeal).Value(); nil == valueErr {
        t.Fatalf("expected the marker-shaped value refused through a wrapped named cipher, stored %q", stored)
    }

    if stored, valueErr := EncryptedDeterministicStringFor[crmCipherReference](otherRowsSeal).Value(); nil == valueErr {
        t.Fatalf("expected the marker-shaped value refused through a wrapped named cipher, stored %q", stored)
    }
}

func TestEncryptedString_APlaintextThroughAWrappedCipherIsSealed(t *testing.T) {
    installCipher(t, defaultCipherName, wrappedCipher{Cipher: NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(7)}}))})

    stored, valueErr := EncryptedString("an ordinary iban").Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan(stored); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "an ordinary iban" != string(loaded) || "an ordinary iban" == string(stored.([]byte)) {
        t.Fatalf("expected the value sealed and read back, stored %q, read %q", stored, string(loaded))
    }
}

type countingKeyProvider struct {
    KeyProvider
    calls int
}

func (instance *countingKeyProvider) Key(keyId string) ([]byte, error) {
    instance.calls++

    return instance.KeyProvider.Key(keyId)
}

func TestCipher_DecryptRefusesAKeyIdOutsideTheGrammarWithoutAskingTheProvider(t *testing.T) {
    provider := &countingKeyProvider{KeyProvider: NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})}
    cipherInstance := NewCipher(provider)

    for _, keyId := range []string{strings.Repeat("k", 4000), "key id", ""} {
        _, decryptErr := cipherInstance.Decrypt(markerPrefix + keyId + ":c2VhbGVkLXBheWxvYWQtb2YtZW5vdWdoLWxlbmd0aA")
        if nil == decryptErr || false == strings.Contains(decryptErr.Error(), "malformed") {
            t.Fatalf("expected the id of %d bytes refused as malformed, got: %v", len(keyId), decryptErr)
        }

        if 200 < len(decryptErr.Error()) {
            t.Fatalf("expected the refusal not to carry the id, got %d bytes", len(decryptErr.Error()))
        }
    }

    if 0 != provider.calls {
        t.Fatalf("expected the provider never asked, got %d lookups", provider.calls)
    }

    inGrammar := strings.Repeat("k", 32)
    _, _ = cipherInstance.Decrypt(markerPrefix + inGrammar + ":c2VhbGVkLXBheWxvYWQtb2YtZW5vdWdoLWxlbmd0aA")
    if 1 != provider.calls {
        t.Fatalf("expected an id in the grammar handed to the provider once, got %d lookups", provider.calls)
    }
}

func newBindingCipher(t *testing.T) *aes256Cipher {
    t.Helper()

    return NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})).(*aes256Cipher)
}

func TestSealAssociatedData_Layout(t *testing.T) {
    expected := "melody/encrypt/seal-binding\x00gcm2\x00v1\x00users.email"
    if associatedData := string(sealAssociatedData(formatGcmV2, "v1", "users.email")); expected != associatedData {
        t.Fatalf("expected %q, got %q", expected, associatedData)
    }
}

/* derived outside the package from the layout and the nonce rule alone, so the wire format, the associated data and the bound nonce are locked at once */
func TestCipher_BoundSealKnownAnswer(t *testing.T) {
    sealed, sealErr := newBindingCipher(t).sealBoundValue("alice@example.com", sealBinding{label: "users.email"}, true)
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if expected := "<ENC>\x00gcm2\x00v1:QvkXvo6EIGKZ/rmT7V+L3USgYdW2/1zsQC3NlaH0ZIuyfNFgxvvcVdJeaRyr"; expected != sealed {
        t.Fatalf("expected the known answer %q, got %q", expected, sealed)
    }
}

/* written by the third major's cipher: an unbound seal of before still opens, and a deterministic unbound seal of today is the same bytes */
func TestCipher_UnboundSealKnownAnswer(t *testing.T) {
    cipherInstance := newBindingCipher(t)
    thirdMajorSeal := "<ENC>\x00gcm1\x00v1:8cyuhVEhdNBBCceRMZoI11bDAOT1L1HOehEfFPR/sh2Rrn1YrXDEyjliXEwx"

    if decrypted, decryptErr := cipherInstance.Decrypt(thirdMajorSeal); nil != decryptErr || "alice@example.com" != decrypted {
        t.Fatalf("expected the third major's seal to open, got %q (%v)", decrypted, decryptErr)
    }

    if sealed, sealErr := cipherInstance.EncryptDeterministic("alice@example.com", EncryptOptions{}); nil != sealErr || thirdMajorSeal != sealed {
        t.Fatalf("expected today's unbound deterministic seal to be the third major's bytes, got %q (%v)", sealed, sealErr)
    }
}

func TestCipher_BoundSealIsAsLongAsTheUnboundOne(t *testing.T) {
    cipherInstance := newBindingCipher(t)

    for _, deterministic := range []bool{false, true} {
        bound, boundErr := cipherInstance.sealBoundValue("alice@example.com", sealBinding{label: "users.email"}, deterministic)
        unbound, unboundErr := cipherInstance.sealValue("alice@example.com", deterministic)
        if nil != boundErr || nil != unboundErr {
            t.Fatalf("seal: %v %v", boundErr, unboundErr)
        }

        if len(bound) != len(unbound) || false == strings.HasPrefix(bound, "<ENC>\x00gcm2\x00") || false == strings.HasPrefix(unbound, "<ENC>\x00gcm1\x00") {
            t.Fatalf("expected equal lengths under gcm2 and gcm1, got %q and %q", bound, unbound)
        }
    }
}

func TestCipher_BoundSealOpensUnderItsBindingAlone(t *testing.T) {
    cipherInstance := newBindingCipher(t)

    sealed, sealErr := cipherInstance.sealBoundValue("alice@example.com", sealBinding{label: "users.email"}, false)
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if opened, openErr := cipherInstance.openStored(sealed, sealBinding{label: "users.email"}); nil != openErr || "alice@example.com" != opened.plaintext {
        t.Fatalf("expected the own label to open, got %q (%v)", opened.plaintext, openErr)
    }

    _, otherErr := cipherInstance.openStored(sealed, sealBinding{label: "leads.email"})
    if nil == otherErr || false == strings.Contains(otherErr.Error(), "could not decrypt value") || "leads.email" != exception.FromError(otherErr).Context()["sealBinding"] {
        t.Fatalf("expected another label to fail authentication naming it, got %v", otherErr)
    }

    if _, decryptErr := cipherInstance.Decrypt(sealed); nil == decryptErr || false == exception.ChainIs(decryptErr, ErrSealBound) {
        t.Fatalf("expected the unbound reader to answer ErrSealBound, got %v", decryptErr)
    }
}

func TestCipher_BoundSealAuthenticatesItsKeyId(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v1copy": newKey(1)}})).(*aes256Cipher)

    bound, _ := cipherInstance.sealBoundValue("alice@example.com", sealBinding{label: "users.email"}, false)
    unbound, _ := cipherInstance.sealValue("alice@example.com", false)

    if _, openErr := cipherInstance.openStored(strings.Replace(bound, "\x00v1:", "\x00v1copy:", 1), sealBinding{label: "users.email"}); nil == openErr {
        t.Fatalf("expected the rewritten key id of a bound seal to fail authentication")
    }

    if decrypted, decryptErr := cipherInstance.Decrypt(strings.Replace(unbound, "\x00v1:", "\x00v1copy:", 1)); nil != decryptErr || "alice@example.com" != decrypted {
        t.Fatalf("expected the rewritten key id of an unbound seal to open, the difference the binding makes, got %q (%v)", decrypted, decryptErr)
    }
}

func TestCipher_BoundDeterministicSealDiffersPerBinding(t *testing.T) {
    cipherInstance := newBindingCipher(t)

    first, _ := cipherInstance.sealBoundValue("alice@example.com", sealBinding{label: "users.email"}, true)
    second, _ := cipherInstance.sealBoundValue("alice@example.com", sealBinding{label: "leads.email"}, true)

    firstPayload := decodedPayload(t, first)
    secondPayload := decodedPayload(t, second)
    nonceSize := minNonceSize
    bodyEnd := len(firstPayload) - gcmTagOverhead

    if true == bytes.Equal(firstPayload[:nonceSize], secondPayload[:nonceSize]) || true == bytes.Equal(firstPayload[nonceSize:bodyEnd], secondPayload[nonceSize:bodyEnd]) {
        t.Fatalf("expected different nonces and bodies for two labels, got %q and %q", first, second)
    }
}

func TestCipher_BoundDeterministicSealDiffersFromTheUnboundOneAndIsStable(t *testing.T) {
    cipherInstance := newBindingCipher(t)
    binding := sealBinding{label: "users.email"}

    bound, _ := cipherInstance.sealBoundValue("alice@example.com", binding, true)
    again, _ := cipherInstance.sealBoundValue("alice@example.com", binding, true)
    unbound, _ := cipherInstance.sealValue("alice@example.com", true)

    if bound != again {
        t.Fatalf("expected equal input to give an equal bound seal, got %q and %q", bound, again)
    }

    if true == bytes.Equal(decodedPayload(t, bound)[:minNonceSize], decodedPayload(t, unbound)[:minNonceSize]) {
        t.Fatalf("expected the bound nonce to differ from the unbound one")
    }

    random, _ := cipherInstance.sealBoundValue("alice@example.com", binding, false)
    for value, deterministic := range map[string]bool{bound: true, random: false} {
        opened, openErr := cipherInstance.openStored(value, binding)
        if nil != openErr || deterministic != opened.deterministic || formatGcmV2 != opened.format {
            t.Fatalf("expected the mode %v read back under gcm2, got %+v (%v)", deterministic, opened, openErr)
        }
    }
}

func TestCipher_DecryptStillOpensAnUnboundSealBesideABoundOne(t *testing.T) {
    cipherInstance := newBindingCipher(t)

    unbound, _ := cipherInstance.sealValue("alice@example.com", false)
    if decrypted, decryptErr := cipherInstance.Decrypt(unbound); nil != decryptErr || "alice@example.com" != decrypted {
        t.Fatalf("expected the unbound seal to open, got %q (%v)", decrypted, decryptErr)
    }
}

func decodedPayload(t *testing.T, sealed string) []byte {
    t.Helper()

    payload, decodeErr := base64.RawStdEncoding.DecodeString(sealed[strings.IndexByte(sealed, ':')+1:])
    if nil != decodeErr {
        t.Fatalf("decode: %v", decodeErr)
    }

    return payload
}

func TestCipher_TheUnboundDoorsSealABoundSealAsData(t *testing.T) {
    cipherInstance := newBindingCipher(t)

    bound, _ := cipherInstance.sealBoundValue("another column's secret", sealBinding{label: "users.email"}, false)

    for name, seal := range map[string]func(string) (string, error){
        "Encrypt":   func(value string) (string, error) { return cipherInstance.Encrypt(value, EncryptOptions{}) },
        "sealValue": func(value string) (string, error) { return cipherInstance.sealValue(value, false) },
    } {
        sealed, sealErr := seal(bound)
        if nil != sealErr || false == strings.HasPrefix(sealed, "<ENC>\x00gcm1\x00") {
            t.Fatalf("%s: expected the bound seal sealed as data, got %q (%v)", name, sealed, sealErr)
        }

        if decrypted, decryptErr := cipherInstance.Decrypt(sealed); nil != decryptErr || bound != decrypted {
            t.Fatalf("%s: expected the bound seal back as data, got %q (%v)", name, decrypted, decryptErr)
        }
    }
}

func TestCipher_OpenStoredReadsTheModeOfAGcm1SealAndNamesTheKeyOfAFailure(t *testing.T) {
    cipherInstance := newBindingCipher(t)
    binding := sealBinding{label: "users.email"}

    deterministic, _ := cipherInstance.sealValue("alice@example.com", true)
    random, _ := cipherInstance.sealValue("alice@example.com", false)
    for value, expected := range map[string]bool{deterministic: true, random: false} {
        opened, openErr := cipherInstance.openStored(value, binding)
        if nil != openErr || expected != opened.deterministic || formatGcmV1 != opened.format || "v1" != opened.keyId {
            t.Fatalf("expected the gcm1 mode %v read back, got %+v (%v)", expected, opened, openErr)
        }
    }

    bound, _ := cipherInstance.sealBoundValue("alice@example.com", binding, false)
    _, openErr := cipherInstance.openStored(bound, sealBinding{label: "leads.email"})
    if context := exception.FromError(openErr).Context(); "v1" != context["keyId"] || "leads.email" != context["sealBinding"] {
        t.Fatalf("expected the failure to name the key id and the binding, got %v", context)
    }
}

func TestCipher_ConcurrentBoundSealsAndOpensShareTheKeyMaterial(t *testing.T) {
    cipherInstance := newBindingCipher(t)
    binding := sealBinding{label: "users.email"}

    var waitGroup sync.WaitGroup
    for worker := 0; worker < 8; worker++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            for iteration := 0; iteration < 50; iteration++ {
                sealed, sealErr := cipherInstance.sealBoundValue("alice@example.com", binding, 0 == iteration%2)
                if nil != sealErr {
                    t.Errorf("seal: %v", sealErr)
                    return
                }

                if opened, openErr := cipherInstance.openStored(sealed, binding); nil != openErr || "alice@example.com" != opened.plaintext {
                    t.Errorf("open: %q %v", opened.plaintext, openErr)
                    return
                }
            }
        }()
    }

    waitGroup.Wait()
}

type movingCurrentKeyProvider struct {
    *StaticKeyProvider
    currentKeyId string
}

func (instance *movingCurrentKeyProvider) CurrentKeyId() string {
    return instance.currentKeyId
}

func newMovingCurrentKeyProvider(currentKeyId string) *movingCurrentKeyProvider {
    return &movingCurrentKeyProvider{
        StaticKeyProvider: NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}),
        currentKeyId:      currentKeyId,
    }
}

func sealedKeyId(t *testing.T, cipherInstance Cipher, sealed string) string {
    t.Helper()

    keyId, markerShaped, keyIdErr := keyIdOf(sealed)
    if nil != keyIdErr || false == markerShaped {
        t.Fatalf("expected a seal, got %q (%v)", sealed, keyIdErr)
    }

    if plaintext, decryptErr := cipherInstance.Decrypt(sealed); nil != decryptErr || "secret" != plaintext {
        t.Fatalf("expected the seal to open to the plaintext, got %q (%v)", plaintext, decryptErr)
    }

    return keyId
}

func TestCipher_EncryptUnderANamedKeySealsUnderIt(t *testing.T) {
    cipherInstance := NewCipher(newMovingCurrentKeyProvider("v2"))

    sealed, sealErr := cipherInstance.Encrypt("secret", EncryptOptions{KeyId: "v1"})
    if nil != sealErr {
        t.Fatalf("encrypt: %v", sealErr)
    }

    if keyId := sealedKeyId(t, cipherInstance, sealed); "v1" != keyId {
        t.Fatalf("expected the seal under the named key v1, got %q", keyId)
    }
}

func TestCipher_EncryptWithoutAKeyIdSealsUnderTheCurrentKey(t *testing.T) {
    provider := newMovingCurrentKeyProvider("v1")
    cipherInstance := NewCipher(provider)

    for _, currentKeyId := range []string{"v1", "v2"} {
        provider.currentKeyId = currentKeyId

        for name, seal := range map[string]func() (string, error){
            "Encrypt":              func() (string, error) { return cipherInstance.Encrypt("secret", EncryptOptions{}) },
            "Encrypt empty id":     func() (string, error) { return cipherInstance.Encrypt("secret", EncryptOptions{KeyId: ""}) },
            "EncryptDeterministic": func() (string, error) { return cipherInstance.EncryptDeterministic("secret", EncryptOptions{}) },
        } {
            sealed, sealErr := seal()
            if nil != sealErr {
                t.Fatalf("%s: %v", name, sealErr)
            }

            if keyId := sealedKeyId(t, cipherInstance, sealed); currentKeyId != keyId {
                t.Fatalf("%s: expected the seal under the current key %q, got %q", name, currentKeyId, keyId)
            }
        }
    }
}

func TestCipher_EncryptDeterministicUnderANamedKeyIsStableAndUnderIt(t *testing.T) {
    cipherInstance := NewCipher(newMovingCurrentKeyProvider("v2"))

    first, firstErr := cipherInstance.EncryptDeterministic("secret", EncryptOptions{KeyId: "v1"})
    second, secondErr := cipherInstance.EncryptDeterministic("secret", EncryptOptions{KeyId: "v1"})
    if nil != firstErr || nil != secondErr {
        t.Fatalf("encrypt deterministic: %v, %v", firstErr, secondErr)
    }

    if first != second {
        t.Fatalf("expected two deterministic seals under one key to be equal, got %q and %q", first, second)
    }

    if keyId := sealedKeyId(t, cipherInstance, first); "v1" != keyId {
        t.Fatalf("expected the deterministic seal under the named key v1, got %q", keyId)
    }
}

type fourMethodCipher struct{}

func (instance fourMethodCipher) Encrypt(plaintext string, options EncryptOptions) (string, error) {
    return plaintext, nil
}

func (instance fourMethodCipher) EncryptDeterministic(plaintext string, options EncryptOptions) (string, error) {
    return plaintext, nil
}

func (instance fourMethodCipher) CiphertextCandidates(plaintext string) ([][]byte, error) {
    return nil, nil
}

func (instance fourMethodCipher) Decrypt(encoded string) (string, error) {
    return encoded, nil
}

func TestCipher_TheInterfaceHasNoKeyIdTwins(t *testing.T) {
    var cipherInstance Cipher = fourMethodCipher{}

    if 4 != reflect.TypeOf((*Cipher)(nil)).Elem().NumMethod() {
        t.Fatalf("expected the cipher contract to hold four methods, got %d", reflect.TypeOf((*Cipher)(nil)).Elem().NumMethod())
    }

    if _, sealErr := cipherInstance.Encrypt("secret", EncryptOptions{}); nil != sealErr {
        t.Fatalf("encrypt: %v", sealErr)
    }
}
