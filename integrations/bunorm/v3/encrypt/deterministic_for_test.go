package encrypt

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
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

/* the searchable compartment-bound column carries the same marker, pinned here for the reason written on its EncryptedStringFor sibling. */
var _ EncryptedColumn = EncryptedDeterministicStringFor[crmCipherRef]("")

func TestEncryptedDeterministicStringFor_UnmarshalJSONRefusesTheRedactionPlaceholder(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedDeterministicStringFor[crmCipherRef]("lookup value"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    decoded := EncryptedDeterministicStringFor[crmCipherRef]("untouched")
    unmarshalErr := json.Unmarshal(payload, &decoded)
    if nil == unmarshalErr {
        t.Fatalf("expected the redacted document to be refused, got %q", string(decoded))
    }

    if false == strings.Contains(fmt.Sprint(exception.LogContext(unmarshalErr)["type"]), "encrypt.EncryptedDeterministicStringFor[") {
        t.Fatalf("expected the refusal to name the column type, got: %v", exception.LogContext(unmarshalErr))
    }

    if "untouched" != string(decoded) {
        t.Fatalf("expected the refused document to leave the value untouched, got %q", string(decoded))
    }
}

func TestEncryptedDeterministicStringFor_UnmarshalJSONDecodesAPlaintextString(t *testing.T) {
    var decoded EncryptedDeterministicStringFor[crmCipherRef]
    if unmarshalErr := json.Unmarshal([]byte(`"hello"`), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if "hello" != string(decoded) {
        t.Fatalf("expected the plaintext to decode, got %q", string(decoded))
    }
}

/* the generic instantiation cannot be asserted in the source file — see the sibling assertion on EncryptedStringFor */
var _ json.Unmarshaler = (*EncryptedDeterministicStringFor[crmCipherRef])(nil)

func TestEncryptedDeterministicStringFor_FormatRedactsNumericVerbs(t *testing.T) {
    secret := "top-secret-plaintext"
    for _, verb := range []string{"%d", "%o", "%b", "%c", "%U"} {
        rendered := fmt.Sprintf(verb, EncryptedDeterministicStringFor[crmCipherRef](secret))
        if true == strings.Contains(rendered, secret) {
            t.Fatalf("verb %s leaked the plaintext through the badverb form: %s", verb, rendered)
        }
        if redactedPlaceholder != rendered {
            t.Fatalf("verb %s expected the redacted placeholder, got %q", verb, rendered)
        }
    }
}
