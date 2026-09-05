package encrypt

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
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

/* emptyNameCipherRef is the misuse the compartment marker must refuse: an empty name is the default cipher's reserved registry entry, not a compartment. */
type emptyNameCipherRef struct{}

func (instance emptyNameCipherRef) CipherName() string {
    return ""
}

/* A marker with an empty CipherName() would resolve the DEFAULT cipher, silently giving a compartment-bound column the default key — the cross-compartment read the marker exists to prevent. */
func TestEncryptedStringFor_EmptyCipherNameIsRejected(t *testing.T) {
    UseCipher(NewFakeCipher())
    defer UseCipher(nil)

    column := EncryptedStringFor[emptyNameCipherRef]("secret")

    if _, valueErr := column.Value(); nil == valueErr {
        t.Fatal("expected an empty cipher name to be rejected rather than resolve the default cipher")
    }

    var scanned EncryptedStringFor[emptyNameCipherRef]
    if scanErr := scanned.Scan("anything"); nil == scanErr {
        t.Fatal("expected Scan with an empty cipher name to be rejected")
    }
}

/* fmt reaches for GoStringer under %#v; without it the underlying string literal — the plaintext — is printed straight into logs and test failures. */
func TestEncryptedTypes_RedactUnderTheGoStringVerb(t *testing.T) {
    const plaintext = "RO49-SECRET-IBAN"

    renderings := []string{
        fmt.Sprintf("%#v", EncryptedString(plaintext)),
        fmt.Sprintf("%#v", EncryptedDeterministicString(plaintext)),
        fmt.Sprintf("%#v", EncryptedStringFor[emptyNameCipherRef](plaintext)),
        fmt.Sprintf("%#v", EncryptedDeterministicStringFor[emptyNameCipherRef](plaintext)),
    }

    for _, rendering := range renderings {
        if true == strings.Contains(rendering, plaintext) {
            t.Fatalf("the plaintext leaked through %%#v: %s", rendering)
        }
    }
}

type pointerFormRef struct{}

func (instance pointerFormRef) CipherName() string {
    return "pointer-form"
}

/* the pointer form compiles whenever the value form does, and its zero value is a nil pointer whose CipherName() dereferences nil from inside database/sql; it must answer as an error naming the marker */
func TestEncryptedStringFor_RefusesAPointerFormMarker(t *testing.T) {
    column := EncryptedStringFor[*pointerFormRef]("plaintext")

    _, valueErr := column.Value()
    if nil == valueErr {
        t.Fatalf("expected the pointer-form marker to be refused")
    }

    if false == strings.Contains(valueErr.Error(), "pointer type") {
        t.Fatalf("expected the refusal to name the pointer form, got: %v", valueErr)
    }
}

/* the compartment-bound column carries the EncryptedColumn marker its non-generic sibling carries, which is what the auto-redaction type walk recognises it by. The assertion lives here rather than beside the type because instantiating a generic column needs a CipherRef marker, and the package deliberately ships none — a marker is the integrator's, minted per compartment. */
var _ EncryptedColumn = EncryptedStringFor[crmCipherRef]("")

func TestEncryptedStringFor_UnmarshalJSONRefusesTheRedactionPlaceholder(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedStringFor[crmCipherRef]("super-secret"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    decoded := EncryptedStringFor[crmCipherRef]("untouched")
    unmarshalErr := json.Unmarshal(payload, &decoded)
    if nil == unmarshalErr {
        t.Fatalf("expected the redacted document to be refused, got %q", string(decoded))
    }

    if false == strings.Contains(fmt.Sprint(exception.LogContext(unmarshalErr)["type"]), "encrypt.EncryptedStringFor[") {
        t.Fatalf("expected the refusal to name the column type, got: %v", exception.LogContext(unmarshalErr))
    }

    if "untouched" != string(decoded) {
        t.Fatalf("expected the refused document to leave the value untouched, got %q", string(decoded))
    }
}

func TestEncryptedStringFor_UnmarshalJSONDecodesAPlaintextString(t *testing.T) {
    var decoded EncryptedStringFor[crmCipherRef]
    if unmarshalErr := json.Unmarshal([]byte(`"hello"`), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if "hello" != string(decoded) {
        t.Fatalf("expected the plaintext to decode, got %q", string(decoded))
    }
}

/* the generic instantiation cannot be asserted in the source file, because the package ships no marker of its own on purpose: the marker is the integrator's, one per compartment */
var _ json.Unmarshaler = (*EncryptedStringFor[crmCipherRef])(nil)
