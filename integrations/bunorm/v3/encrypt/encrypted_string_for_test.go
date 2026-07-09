package encrypt

import (
    "encoding/json"
    "strings"
    "testing"
)

type crmCipherRef struct{}

func (instance crmCipherRef) CipherName() string {
    return "test:crm"
}

type billingCipherRef struct{}

func (instance billingCipherRef) CipherName() string {
    return "test:billing"
}

func useCompartmentCiphers(t *testing.T) {
    t.Helper()

    crmProvider := NewStaticKeyProvider("crm-v1", map[string][]byte{"crm-v1": newKey(11)})
    billingProvider := NewStaticKeyProvider("billing-v1", map[string][]byte{"billing-v1": newKey(23)})

    UseCipherNamed("test:crm", NewCipher(crmProvider))
    UseCipherNamed("test:billing", NewCipher(billingProvider))

    t.Cleanup(func() {
        storeCipher("test:crm", nil)
        storeCipher("test:billing", nil)
    })
}

func TestEncryptedStringFor_RoundTripPerCompartment(t *testing.T) {
    useCompartmentCiphers(t)

    original := EncryptedStringFor[crmCipherRef]("customer iban")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes || "customer iban" == string(storedBytes) {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded EncryptedStringFor[crmCipherRef]
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "customer iban" != string(loaded) {
        t.Fatalf("expected the round trip to restore the plaintext, got %q", string(loaded))
    }
}

func TestEncryptedStringFor_CompartmentsAreIsolated(t *testing.T) {
    useCompartmentCiphers(t)

    crmValue := EncryptedStringFor[crmCipherRef]("customer iban")

    stored, valueErr := crmValue.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    /* the billing compartment must not be able to decrypt a crm ciphertext */
    var leaked EncryptedStringFor[billingCipherRef]
    if scanErr := leaked.Scan(stored.([]byte)); nil == scanErr {
        t.Fatalf("expected the billing compartment to reject a crm ciphertext")
    }
}

func TestEncryptedStringFor_UnconfiguredCompartmentErrors(t *testing.T) {
    value := EncryptedStringFor[crmCipherRef]("data")

    if _, valueErr := value.Value(); nil == valueErr {
        t.Fatalf("expected an error without the named cipher installed")
    }
}

func TestEncryptedStringFor_DoesNotUseTheDefaultCipher(t *testing.T) {
    UseCipher(NewFakeCipher())
    defer UseCipher(nil)

    value := EncryptedStringFor[crmCipherRef]("data")

    if _, valueErr := value.Value(); nil == valueErr {
        t.Fatalf("expected the named binding to ignore the default cipher")
    }
}

func TestEncryptedStringFor_RedactsEverywhere(t *testing.T) {
    value := EncryptedStringFor[crmCipherRef]("secret")

    if redactedPlaceholder != value.String() {
        t.Fatalf("expected String to redact, got %q", value.String())
    }

    payload, marshalErr := json.Marshal(value)
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "secret") {
        t.Fatalf("plaintext leaked through json: %s", payload)
    }

    if redactedPlaceholder != value.LogValue().String() {
        t.Fatalf("expected LogValue to redact, got %q", value.LogValue().String())
    }
}

func TestEncryptedStringFor_ScanNullResetsTheValue(t *testing.T) {
    useCompartmentCiphers(t)

    loaded := EncryptedStringFor[crmCipherRef]("stale")
    if scanErr := loaded.Scan(nil); nil != scanErr {
        t.Fatalf("scan nil: %v", scanErr)
    }

    if "" != string(loaded) {
        t.Fatalf("expected the null scan to clear the value, got %q", string(loaded))
    }
}

func TestEncryptedStringFor_RotationInsideTheCompartmentKeepsDecrypting(t *testing.T) {
    oldKey := newKey(31)

    provider := NewStaticKeyProvider("crm-v1", map[string][]byte{"crm-v1": oldKey})
    UseCipherNamed("test:crm", NewCipher(provider))
    t.Cleanup(func() {
        storeCipher("test:crm", nil)
    })

    original := EncryptedStringFor[crmCipherRef]("long lived row")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    /* rotate: a new current key, the old key still active for decryption */
    rotatedProvider := NewStaticKeyProvider("crm-v2", map[string][]byte{
        "crm-v1": oldKey,
        "crm-v2": newKey(37),
    })
    UseCipherNamed("test:crm", NewCipher(rotatedProvider))

    var loaded EncryptedStringFor[crmCipherRef]
    if scanErr := loaded.Scan(stored.([]byte)); nil != scanErr {
        t.Fatalf("scan after rotation: %v", scanErr)
    }

    if "long lived row" != string(loaded) {
        t.Fatalf("expected the rotated compartment to decrypt the old row, got %q", string(loaded))
    }
}
