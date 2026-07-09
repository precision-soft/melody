package encrypt

import (
    "testing"
)

func TestEncryptedDeterministicStringFor_EqualPlaintextsProduceEqualCiphertexts(t *testing.T) {
    useCompartmentCiphers(t)

    first := EncryptedDeterministicStringFor[crmCipherRef]("lookup value")
    second := EncryptedDeterministicStringFor[crmCipherRef]("lookup value")

    firstStored, firstErr := first.Value()
    secondStored, secondErr := second.Value()
    if nil != firstErr || nil != secondErr {
        t.Fatalf("value: %v %v", firstErr, secondErr)
    }

    if string(firstStored.([]byte)) != string(secondStored.([]byte)) {
        t.Fatalf("expected deterministic ciphertexts to be equal")
    }
}

func TestEncryptedDeterministicStringFor_RoundTripAndIsolation(t *testing.T) {
    useCompartmentCiphers(t)

    original := EncryptedDeterministicStringFor[crmCipherRef]("searchable")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    var loaded EncryptedDeterministicStringFor[crmCipherRef]
    if scanErr := loaded.Scan(stored.([]byte)); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "searchable" != string(loaded) {
        t.Fatalf("expected the round trip to restore the plaintext, got %q", string(loaded))
    }

    var leaked EncryptedDeterministicStringFor[billingCipherRef]
    if scanErr := leaked.Scan(stored.([]byte)); nil == scanErr {
        t.Fatalf("expected the billing compartment to reject a crm ciphertext")
    }
}

func TestEncryptedDeterministicStringFor_RedactsString(t *testing.T) {
    value := EncryptedDeterministicStringFor[crmCipherRef]("secret")

    if redactedPlaceholder != value.String() {
        t.Fatalf("expected String to redact, got %q", value.String())
    }
}
