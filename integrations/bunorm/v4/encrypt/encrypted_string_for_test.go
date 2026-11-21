package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

type crmCipherReference struct{}

func (instance crmCipherReference) CipherName() string {
    return "test:crm"
}

type billingCipherReference struct{}

func (instance billingCipherReference) CipherName() string {
    return "test:billing"
}

func useCompartmentCiphers(t *testing.T) {
    t.Helper()

    crmProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v1", Keys: map[string][]byte{"crm-v1": newKey(11)}})
    billingProvider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "billing-v1", Keys: map[string][]byte{"billing-v1": newKey(23)}})

    installCipher(t, "test:crm", NewCipher(crmProvider))
    installCipher(t, "test:billing", NewCipher(billingProvider))
}

func TestEncryptedStringFor_RoundTripPerCompartment(t *testing.T) {
    useCompartmentCiphers(t)

    original := EncryptedStringFor[crmCipherReference]("customer iban")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes || "customer iban" == string(storedBytes) {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded EncryptedStringFor[crmCipherReference]
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "customer iban" != string(loaded) {
        t.Fatalf("expected the round trip to restore the plaintext, got %q", string(loaded))
    }
}

func TestEncryptedStringFor_CompartmentsAreIsolated(t *testing.T) {
    useCompartmentCiphers(t)

    crmValue := EncryptedStringFor[crmCipherReference]("customer iban")

    stored, valueErr := crmValue.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    /* the billing compartment must not be able to decrypt a crm ciphertext */
    var leaked EncryptedStringFor[billingCipherReference]
    if scanErr := leaked.Scan(stored.([]byte)); nil == scanErr {
        t.Fatalf("expected the billing compartment to reject a crm ciphertext")
    }
}

func TestEncryptedStringFor_UnconfiguredCompartmentErrors(t *testing.T) {
    value := EncryptedStringFor[crmCipherReference]("data")

    if _, valueErr := value.Value(); nil == valueErr {
        t.Fatalf("expected an error without the named cipher installed")
    }
}

func TestEncryptedStringFor_DoesNotUseTheDefaultCipher(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())

    value := EncryptedStringFor[crmCipherReference]("data")

    if _, valueErr := value.Value(); nil == valueErr {
        t.Fatalf("expected the named binding to ignore the default cipher")
    }
}

func TestEncryptedStringFor_RedactsEverywhere(t *testing.T) {
    value := EncryptedStringFor[crmCipherReference]("secret")

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

    loaded := EncryptedStringFor[crmCipherReference]("stale")
    if scanErr := loaded.Scan(nil); nil != scanErr {
        t.Fatalf("scan nil: %v", scanErr)
    }

    if "" != string(loaded) {
        t.Fatalf("expected the null scan to clear the value, got %q", string(loaded))
    }
}

func TestEncryptedStringFor_RotationInsideTheCompartmentKeepsDecrypting(t *testing.T) {
    oldKey := newKey(31)

    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v1", Keys: map[string][]byte{"crm-v1": oldKey}})
    installCipher(t, "test:crm", NewCipher(provider))

    original := EncryptedStringFor[crmCipherReference]("long lived row")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    /* rotate: a new current key, the retired key still active for decryption */
    rotatedProvider := NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "crm-v2",
        Keys: map[string][]byte{
            "crm-v1": oldKey,
            "crm-v2": newKey(37),
        },
    })
    storeCipher("test:crm", NewCipher(rotatedProvider))

    var loaded EncryptedStringFor[crmCipherReference]
    if scanErr := loaded.Scan(stored.([]byte)); nil != scanErr {
        t.Fatalf("scan after rotation: %v", scanErr)
    }

    if "long lived row" != string(loaded) {
        t.Fatalf("expected the rotated compartment to decrypt the old row, got %q", string(loaded))
    }
}

type emptyNameCipherReference struct{}

func (instance emptyNameCipherReference) CipherName() string {
    return ""
}

/* A marker with an empty CipherName() would resolve the DEFAULT cipher, silently giving a compartment-bound column the default key — the cross-compartment read the marker exists to prevent. */
func TestEncryptedStringFor_EmptyCipherNameIsRejected(t *testing.T) {
    installCipher(t, defaultCipherName, NewFakeCipher())

    column := EncryptedStringFor[emptyNameCipherReference]("secret")

    if _, valueErr := column.Value(); nil == valueErr {
        t.Fatal("expected an empty cipher name to be rejected rather than resolve the default cipher")
    }

    var scanned EncryptedStringFor[emptyNameCipherReference]
    if scanErr := scanned.Scan("anything"); nil == scanErr {
        t.Fatal("expected Scan with an empty cipher name to be rejected")
    }
}

func TestEncryptedTypes_RedactUnderTheGoStringVerb(t *testing.T) {
    const plaintext = "RO49-SECRET-IBAN"

    renderings := []string{
        fmt.Sprintf("%#v", EncryptedString(plaintext)),
        fmt.Sprintf("%#v", EncryptedDeterministicString(plaintext)),
        fmt.Sprintf("%#v", EncryptedStringFor[emptyNameCipherReference](plaintext)),
        fmt.Sprintf("%#v", EncryptedDeterministicStringFor[emptyNameCipherReference](plaintext)),
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

    if false == strings.Contains(valueErr.Error(), "pointer or interface type") {
        t.Fatalf("expected the refusal to name the pointer form, got: %v", valueErr)
    }
}

var _ EncryptedColumn = EncryptedStringFor[crmCipherReference]("")

func TestEncryptedStringFor_UnmarshalJSONRefusesTheRedactionPlaceholder(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedStringFor[crmCipherReference]("super-secret"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    decoded := EncryptedStringFor[crmCipherReference]("untouched")
    unmarshalErr := json.Unmarshal(payload, &decoded)
    if nil == unmarshalErr {
        t.Fatalf("expected the redacted document to be refused, got %q", string(decoded))
    }

    if false == strings.Contains(fmt.Sprint(exception.LogContext(unmarshalErr, exception.LogContextConfig{})["type"]), "encrypt.EncryptedStringFor[") {
        t.Fatalf("expected the refusal to name the column type, got: %v", exception.LogContext(unmarshalErr, exception.LogContextConfig{}))
    }

    if "untouched" != string(decoded) {
        t.Fatalf("expected the refused document to leave the value untouched, got %q", string(decoded))
    }
}

func TestEncryptedStringFor_UnmarshalJSONDecodesAPlaintextString(t *testing.T) {
    var decoded EncryptedStringFor[crmCipherReference]
    if unmarshalErr := json.Unmarshal([]byte(`"hello"`), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if "hello" != string(decoded) {
        t.Fatalf("expected the plaintext to decode, got %q", string(decoded))
    }
}

/* the generic instantiation cannot be asserted in the source file, because the package ships no marker of its own on purpose: the marker is the integrator's, one per compartment */
var _ json.Unmarshaler = (*EncryptedStringFor[crmCipherReference])(nil)

func TestEncryptedStringFor_FormatRedactsNumericVerbs(t *testing.T) {
    secret := "top-secret-plaintext"
    for _, verb := range []string{"%d", "%o", "%b", "%c", "%U"} {
        rendered := fmt.Sprintf(verb, EncryptedStringFor[crmCipherReference](secret))
        if true == strings.Contains(rendered, secret) {
            t.Fatalf("verb %s leaked the plaintext through the badverb form: %s", verb, rendered)
        }
        if redactedPlaceholder != rendered {
            t.Fatalf("verb %s expected the redacted placeholder, got %q", verb, rendered)
        }
    }
}

/* a column an attacker can write and later read back must not decrypt a seal handed to it as data: the seal of another row's secret, stored through this column, is read back as that seal and never as the secret */
func TestEncryptedStringFor_ASealWrittenAsDataIsSealedAndReadBackAsTheSeal(t *testing.T) {
    useCompartmentCiphers(t)

    crmCipher, _, cipherErr := referenceColumn[crmCipherReference]()
    if nil != cipherErr {
        t.Fatalf("cipher: %v", cipherErr)
    }

    otherRowsSeal, sealErr := crmCipher.Encrypt("another customer's iban")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    stored, valueErr := EncryptedStringFor[crmCipherReference](otherRowsSeal).Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    if otherRowsSeal == string(stored.([]byte)) {
        t.Fatalf("expected the seal to be sealed as data, it was stored verbatim")
    }

    var loaded EncryptedStringFor[crmCipherReference]
    if scanErr := loaded.Scan(stored); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if otherRowsSeal != string(loaded) {
        t.Fatalf("expected the column to read back the seal it was given, got %q", string(loaded))
    }
}

func TestEncryptedStringFor_AnInterfaceMarkerIsRefusedWithoutAPanic(t *testing.T) {
    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected an error, the marker panicked: %v", recovered)
        }
    }()

    if _, valueErr := EncryptedStringFor[CipherReference]("plaintext").Value(); nil == valueErr || false == strings.Contains(valueErr.Error(), "pointer or interface type") {
        t.Fatalf("expected the interface marker refused at Value, got: %v", valueErr)
    }

    var loaded EncryptedStringFor[CipherReference]
    if scanErr := loaded.Scan(markerPrefix + "v1:c2VhbGVk"); nil == scanErr {
        t.Fatal("expected the interface marker refused at Scan")
    }
}

/* the refusal of a marker that is a pointer or an interface names the marker's TYPE: %T of a nil interface spelled "<nil>", which named nothing. */
func TestReferenceCipher_RefusalNamesThePointerAndTheInterfaceMarker(t *testing.T) {
    _, _, interfaceErr := referenceColumn[CipherReference]()
    interfaceRefusal, isInterfaceRefusal := interfaceErr.(*exception.Error)
    if false == isInterfaceRefusal || "encrypt.CipherReference" != interfaceRefusal.Context()["cipherReference"] {
        t.Fatalf("expected the interface marker named, got %v", interfaceErr)
    }

    _, _, pointerErr := referenceColumn[*crmCipherReference]()
    pointerRefusal, isPointerRefusal := pointerErr.(*exception.Error)
    if false == isPointerRefusal || "*encrypt.crmCipherReference" != pointerRefusal.Context()["cipherReference"] {
        t.Fatalf("expected the pointer marker named, got %v", pointerErr)
    }
}

/* a bound marker's column writes gcm2 under its label, whatever the switch says, and reads it back */
func TestEncryptedStringFor_ValueSealsUnderTheBindingOfItsMarker(t *testing.T) {
    cipherInstance := newCrmCipher(t)

    for _, refuseUnbound := range []bool{false, true} {
        installSealBinding(t, crmIbanBinding{}, refuseUnbound)

        for name, value := range map[string]func() (driver.Value, error){
            "random":        EncryptedStringFor[crmIbanBinding]("RO49AAAA1B31007593840000").Value,
            "deterministic": EncryptedDeterministicStringFor[crmIbanBinding]("RO49AAAA1B31007593840000").Value,
        } {
            stored, valueErr := value()
            if nil != valueErr || false == strings.HasPrefix(string(stored.([]byte)), "<ENC>\x00gcm2\x00crm-v2:") {
                t.Fatalf("%s refuseUnbound=%v: expected a gcm2 seal under the current key, got %q (%v)", name, refuseUnbound, stored, valueErr)
            }

            opened, openErr := cipherInstance.(*aes256Cipher).openStored(string(stored.([]byte)), sealBinding{label: "customer.iban"})
            if nil != openErr || "RO49AAAA1B31007593840000" != opened.plaintext {
                t.Fatalf("%s: expected the seal to open under the label, got %q (%v)", name, opened.plaintext, openErr)
            }

            var scanned EncryptedStringFor[crmIbanBinding]
            if scanErr := scanned.Scan(stored); nil != scanErr || "RO49AAAA1B31007593840000" != string(scanned) {
                t.Fatalf("%s: expected the column to read it back, got %q (%v)", name, string(scanned), scanErr)
            }
        }
    }
}

/* a marker without a binding is unchanged: its column still writes gcm1 */
func TestEncryptedStringFor_AMarkerWithoutABindingStillSealsGcm1(t *testing.T) {
    newCrmCipher(t)

    stored, valueErr := EncryptedStringFor[crmCipherReference]("RO49AAAA1B31007593840000").Value()
    if nil != valueErr || false == strings.HasPrefix(string(stored.([]byte)), "<ENC>\x00gcm1\x00") {
        t.Fatalf("expected a gcm1 seal, got %q (%v)", stored, valueErr)
    }
}

/* while the binding admits them, a gcm1 seal and a value without the marker are read */
func TestBoundColumn_AdmitsAnUnboundSealAndPlaintextByDefault(t *testing.T) {
    cipherInstance := newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, false)

    unbound, _ := cipherInstance.Encrypt("RO49AAAA1B31007593840000")

    for _, stored := range []string{unbound, "RO49AAAA1B31007593840000"} {
        var scanned EncryptedStringFor[crmIbanBinding]
        if scanErr := scanned.Scan([]byte(stored)); nil != scanErr || "RO49AAAA1B31007593840000" != string(scanned) {
            t.Fatalf("expected %q admitted, got %q (%v)", stored, string(scanned), scanErr)
        }
    }
}

/* a binding that refuses unbound values refuses a gcm1 seal on its format alone, before any key is asked for */
func TestBoundColumn_RefusingUnboundRefusesAGcm1Seal(t *testing.T) {
    provider := &countingKeyProvider{KeyProvider: NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v2", Keys: map[string][]byte{"crm-v2": newKey(12)}})}
    cipherInstance := NewCipher(provider)
    installCipher(t, crmCipherReference{}.CipherName(), cipherInstance)
    installSealBinding(t, crmIbanBinding{}, true)

    unbound, _ := cipherInstance.Encrypt("RO49AAAA1B31007593840000")
    provider.calls = 0

    var scanned EncryptedStringFor[crmIbanBinding]
    scanErr := scanned.Scan([]byte(unbound))
    if nil == scanErr || false == exception.ChainIs(scanErr, ErrUnboundValueRefused) || "customer.iban" != exception.FromError(scanErr).Context()["sealBinding"] {
        t.Fatalf("expected ErrUnboundValueRefused naming the binding, got %v", scanErr)
    }

    if 0 != provider.calls {
        t.Fatalf("expected no key asked for, got %d lookups", provider.calls)
    }
}

/* the same switch refuses a value without the marker, so a bare value written into the column is not served */
func TestBoundColumn_RefusingUnboundRefusesAValueWithoutTheMarker(t *testing.T) {
    newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, true)

    var scanned EncryptedDeterministicStringFor[crmIbanBinding]
    if scanErr := scanned.Scan([]byte("RO49AAAA1B31007593840000")); nil == scanErr || false == exception.ChainIs(scanErr, ErrUnboundValueRefused) {
        t.Fatalf("expected ErrUnboundValueRefused, got %v", scanErr)
    }
}

/* a bound column reads the marker family as the plain ones do: a format this build does not read is ErrSealFormatUnknown, while the binding admits unbound values too */
func TestBoundColumn_RefusesAnUnknownSealFormat(t *testing.T) {
    newCrmCipher(t)

    for _, refuseUnbound := range []bool{false, true} {
        installSealBinding(t, crmIbanBinding{}, refuseUnbound)

        var scanned EncryptedStringFor[crmIbanBinding]
        if scanErr := scanned.Scan([]byte("<ENC>\x00gcm9\x00crm-v2:zzz")); nil == scanErr || false == exception.ChainIs(scanErr, ErrSealFormatUnknown) {
            t.Fatalf("refuseUnbound=%v: expected ErrSealFormatUnknown, got %v", refuseUnbound, scanErr)
        }
    }
}

type pointerReceiverBinding struct{}

func (instance pointerReceiverBinding) CipherName() string {
    return "test:crm"
}

func (instance *pointerReceiverBinding) SealBinding() string {
    return "customer.pointer_receiver"
}

func TestReferenceColumn_RefusesAMarkerThatCannotCarryItsBinding(t *testing.T) {
    newCrmCipher(t)
    installCipher(t, defaultCipherName, NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})))

    if stored, valueErr := EncryptedStringFor[pointerReceiverBinding]("value").Value(); nil == valueErr || false == strings.Contains(valueErr.Error(), "pointer receiver") {
        t.Fatalf("expected the pointer-receiver marker refused, stored %q (%v)", stored, valueErr)
    }

    if stored, valueErr := EncryptedStringFor[pointerDefaultBinding]("value").Value(); nil == valueErr || false == strings.Contains(valueErr.Error(), "by pointer") {
        t.Fatalf("expected DefaultCipherReference by pointer refused, stored %q (%v)", stored, valueErr)
    }
}

/* a seal written through one marker does not read through another marker of the same compartment */
func TestBoundColumn_RefusesASealRelocatedFromAnotherBinding(t *testing.T) {
    newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, false)
    installSealBinding(t, crmEmailBinding{}, false)

    stored, _ := EncryptedStringFor[crmIbanBinding]("RO49AAAA1B31007593840000").Value()

    var relocated EncryptedStringFor[crmEmailBinding]
    if scanErr := relocated.Scan(stored); nil == scanErr || false == strings.Contains(scanErr.Error(), "could not decrypt value") {
        t.Fatalf("expected the relocated seal refused, got %q (%v)", string(relocated), scanErr)
    }

    var own EncryptedStringFor[crmIbanBinding]
    if scanErr := own.Scan(stored); nil != scanErr || "RO49AAAA1B31007593840000" != string(own) {
        t.Fatalf("expected the seal to read through its own marker, got %q (%v)", string(own), scanErr)
    }
}

/* a value that is itself a seal, of either format, is sealed as data under the binding and reads back as that string */
func TestEncryptedStringFor_ASealWrittenAsDataRoundTripsUnderABinding(t *testing.T) {
    cipherInstance := newCrmCipher(t)
    installSealBinding(t, crmIbanBinding{}, true)
    installSealBinding(t, crmEmailBinding{}, false)

    unbound, _ := cipherInstance.Encrypt("another customer's iban")
    bound, _ := EncryptedStringFor[crmEmailBinding]("another customer's email").Value()

    for _, applicationValue := range []string{unbound, string(bound.([]byte))} {
        stored, valueErr := EncryptedStringFor[crmIbanBinding](applicationValue).Value()
        if nil != valueErr {
            t.Fatalf("value: %v", valueErr)
        }

        var scanned EncryptedStringFor[crmIbanBinding]
        if scanErr := scanned.Scan(stored); nil != scanErr || applicationValue != string(scanned) {
            t.Fatalf("expected the seal stored as data to read back as itself, got %q (%v)", string(scanned), scanErr)
        }
    }
}

/* a bound marker that was never declared is refused at its first use, naming the marker and the field that declares it */
func TestEncryptedStringFor_AnUndeclaredBindingIsRefused(t *testing.T) {
    newCrmCipher(t)

    _, valueErr := EncryptedStringFor[undeclaredBinding]("value").Value()

    var scanned EncryptedDeterministicStringFor[undeclaredBinding]
    scanErr := scanned.Scan([]byte("value"))

    for _, refusal := range []error{valueErr, scanErr} {
        if nil == refusal {
            t.Fatalf("expected the undeclared binding refused")
        }

        context := exception.FromError(refusal).Context()
        if "ModuleConfig.SealBindings" != context["field"] || false == strings.Contains(fmt.Sprint(context["cipherReference"]), "undeclaredBinding") {
            t.Fatalf("expected the refusal to name the marker and the field, got %v", context)
        }
    }
}

/* a wrapped cipher installed behind the declaration's back is refused by the bound column, which never seals unbound in its place */
func TestBoundColumn_AWrappedCipherIsRefused(t *testing.T) {
    installCipher(t, crmCipherReference{}.CipherName(), wrappedCipher{Cipher: NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v2", Keys: map[string][]byte{"crm-v2": newKey(12)}}))})
    installSealBinding(t, crmIbanBinding{}, false)

    if stored, valueErr := EncryptedStringFor[crmIbanBinding]("value").Value(); nil == valueErr || false == strings.Contains(valueErr.Error(), "needs the package cipher") {
        t.Fatalf("expected the wrapped cipher refused, stored %q (%v)", stored, valueErr)
    }

    var scanned EncryptedStringFor[crmIbanBinding]
    if scanErr := scanned.Scan([]byte("value")); nil == scanErr || false == strings.Contains(scanErr.Error(), "needs the package cipher") {
        t.Fatalf("expected the wrapped cipher refused on Scan, got %v", scanErr)
    }
}

func TestDefaultCipherReference_SelectsTheDefaultCipher(t *testing.T) {
    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))
    installCipher(t, defaultCipherName, cipherInstance)
    installSealBinding(t, defaultSecretBinding{}, true)

    stored, valueErr := EncryptedStringFor[defaultSecretBinding]("totp secret").Value()
    if nil != valueErr || false == strings.HasPrefix(string(stored.([]byte)), "<ENC>\x00gcm2\x00v1:") {
        t.Fatalf("expected a gcm2 seal under the default cipher, got %q (%v)", stored, valueErr)
    }

    if _, _, resolveErr := referenceColumn[emptyNameRef](); nil == resolveErr || false == strings.Contains(resolveErr.Error(), "name is empty") {
        t.Fatalf("expected the empty name refused without DefaultCipherReference, got %v", resolveErr)
    }
}

type emptyNameRef struct{}

func (instance emptyNameRef) CipherName() string {
    return ""
}
