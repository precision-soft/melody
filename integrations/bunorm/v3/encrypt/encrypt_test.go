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

    if false == strings.HasPrefix(encoded, "v1:") {
        t.Fatalf("expected key id prefix, got %q", encoded)
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

func TestEncryptedString_ValueScanRoundTrip(t *testing.T) {
    provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    encrypt.UseCipher(encrypt.NewCipher(provider))
    defer encrypt.UseCipher(nil)

    original := encrypt.EncryptedString("personal data")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedString, isString := stored.(string)
    if false == isString || "personal data" == storedString {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded encrypt.EncryptedString
    if scanErr := loaded.Scan(storedString); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "personal data" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
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
