package encrypt

import (
    "bytes"
    "testing"
)

func deterministicCandidateMatches(t *testing.T, cipher Cipher, plaintext string, encrypted string) bool {
    t.Helper()

    candidates, candidatesErr := cipher.CiphertextCandidates(plaintext)
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    for _, candidate := range candidates {
        if true == bytes.Equal(candidate, []byte(encrypted)) {
            return true
        }
    }

    return false
}

func newInternalKey(seed byte) []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = seed
    }

    return key
}

func TestKeyIdOf_ReportsKeyForEncryptedValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newInternalKey(1), "v2": newInternalKey(2)})
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
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newInternalKey(1), "v2": newInternalKey(2)})
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

func TestEncryptTransform_DeterministicProducesSearchableCiphertext(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newInternalKey(1), "v2": newInternalKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministic, transformErr := migrator.encryptTransform(TableSpec{Deterministic: true})("alice@example.com")
    if nil != transformErr {
        t.Fatalf("deterministic encrypt transform: %v", transformErr)
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministic) {
        t.Fatalf("expected the deterministic encrypt transform to be searchable via CiphertextCandidates")
    }

    randomized, randomizedErr := migrator.encryptTransform(TableSpec{Deterministic: false})("alice@example.com")
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt transform: %v", randomizedErr)
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomized) {
        t.Fatalf("expected the randomized encrypt transform to not match deterministic candidates")
    }
}

func TestReencryptTransform_ConvertsRandomizedSameKeyValueToDeterministic(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newInternalKey(1), "v2": newInternalKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomizedUnderTarget, _ := cipher.EncryptWithKeyId("alice@example.com", "v2")
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomizedUnderTarget) {
        t.Fatalf("precondition: randomized value should not be searchable")
    }

    converted, convertErr := migrator.reencryptTransform(TableSpec{Deterministic: true}, "v2")(randomizedUnderTarget)
    if nil != convertErr {
        t.Fatalf("deterministic reencrypt transform: %v", convertErr)
    }
    if converted == randomizedUnderTarget {
        t.Fatalf("expected a deterministic reencrypt to convert a randomized same-key value rather than skip it")
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", converted) {
        t.Fatalf("expected the converted value to be searchable via CiphertextCandidates")
    }

    skipped, skipErr := migrator.reencryptTransform(TableSpec{Deterministic: false}, "v2")(randomizedUnderTarget)
    if nil != skipErr {
        t.Fatalf("randomized reencrypt transform: %v", skipErr)
    }
    if skipped != randomizedUnderTarget {
        t.Fatalf("expected a randomized same-key reencrypt to keep the fast-path skip")
    }
}

func TestReencryptTransform_RandomizedSameKeyRewritesDeterministicValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newInternalKey(1), "v2": newInternalKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministicUnderTarget, _ := cipher.EncryptDeterministicWithKeyId("alice@example.com", "v2")
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministicUnderTarget) {
        t.Fatalf("precondition: deterministic value should be searchable")
    }

    rewritten, rewriteErr := migrator.reencryptTransform(TableSpec{Deterministic: false}, "v2")(deterministicUnderTarget)
    if nil != rewriteErr {
        t.Fatalf("randomized reencrypt transform: %v", rewriteErr)
    }
    if rewritten == deterministicUnderTarget {
        t.Fatalf("expected a randomized reencrypt to rewrite a deterministic same-key value, but it was skipped")
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", rewritten) {
        t.Fatalf("expected the rewritten value to no longer be searchable via CiphertextCandidates")
    }
    if plaintext, _ := cipher.Decrypt(rewritten); "alice@example.com" != plaintext {
        t.Fatalf("expected the rewritten value to still decrypt to the original plaintext")
    }
}
