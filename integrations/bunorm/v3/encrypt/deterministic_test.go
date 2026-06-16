package encrypt

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestEncryptedDeterministicString_MarshalJSONRedactsPlaintext(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedDeterministicString("super-secret"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "super-secret") {
        t.Fatalf("plaintext leaked through json: %s", payload)
    }

    var decoded string
    if unmarshalErr := json.Unmarshal(payload, &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }
    if "<redacted>" != decoded {
        t.Fatalf("expected redacted json, got %q", decoded)
    }
}

func TestEncryptedDeterministicString_ValuePreservesMarkerBytes(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    UseCipher(NewCipher(provider))
    defer UseCipher(nil)

    original := EncryptedDeterministicString("alice@example.com")

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

    var loaded EncryptedDeterministicString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "alice@example.com" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}
