package encrypt

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"
)

func TestEncryptedString_MarshalJSONRedactsPlaintext(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedString("super-secret"))
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

func TestEncryptedString_MarshalJSONRedactsWhenNested(t *testing.T) {
    type holder struct {
        Email EncryptedString   `json:"email"`
        Tags  []EncryptedString `json:"tags"`
    }

    payload, marshalErr := json.Marshal(holder{Email: "ada@example.com", Tags: []EncryptedString{"tag-secret"}})
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "ada@example.com") || true == strings.Contains(string(payload), "tag-secret") {
        t.Fatalf("nested plaintext leaked through json: %s", payload)
    }
}

func TestEncryptedString_ValueScanRoundTrip(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    UseCipher(NewCipher(provider))
    defer UseCipher(nil)

    original := EncryptedString("personal data")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes || "personal data" == string(storedBytes) {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "personal data" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}

func TestEncryptedString_FailsClosedWithoutCipher(t *testing.T) {
    UseCipher(nil)

    secret := EncryptedString("personal data")
    if _, valueErr := secret.Value(); nil == valueErr {
        t.Fatalf("expected Value to fail when no cipher is configured")
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan("anything"); nil == scanErr {
        t.Fatalf("expected Scan to fail when no cipher is configured")
    }

    if scanErr := loaded.Scan(nil); nil != scanErr {
        t.Fatalf("scan of NULL should succeed: %v", scanErr)
    }
}

func TestEncryptedString_MasksPlaintextWhenFormatted(t *testing.T) {
    secret := EncryptedString("personal data")

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
