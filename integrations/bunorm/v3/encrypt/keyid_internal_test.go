package encrypt

import (
    "testing"
)

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

    /** a naive decrypt+re-encrypt produces a different ciphertext (fresh random nonce), which is why
        MigrateReencrypt skips rows whose key id already matches the target */
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
