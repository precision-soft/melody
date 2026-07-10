package main

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

/* crmCipherRef and billingCipherRef are the zero-size markers that bind a column type to a key compartment;
each names a cipher installed with encrypt.UseCipherNamed. */
type crmCipherRef struct{}

func (instance crmCipherRef) CipherName() string {
    return "crm"
}

type billingCipherRef struct{}

func (instance billingCipherRef) CipherName() string {
    return "billing"
}

type unregisteredCipherRef struct{}

func (instance unregisteredCipherRef) CipherName() string {
    return "unregistered"
}

/* runEncryptCompartmentCheck exercises the named-cipher registry: two compartments each own their key
provider, so a column bound to one compartment round-trips through it and its ciphertext is undecryptable by
the other — the isolation guarantee that merging every key into one provider would silently lose. It also
asserts the value never leaks in a Stringer, log or JSON rendering, and that an unregistered compartment
errors rather than falling back to the default cipher. */
func runEncryptCompartmentCheck() {
    crmCipher := encrypt.NewCipher(encrypt.NewStaticKeyProvider("crm-v1", map[string][]byte{
        "crm-v1": []byte("0123456789abcdef0123456789abcdef"),
    }))
    billingCipher := encrypt.NewCipher(encrypt.NewStaticKeyProvider("billing-v1", map[string][]byte{
        "billing-v1": []byte("fedcba9876543210fedcba9876543210"),
    }))

    encrypt.UseCipherNamed("crm", crmCipher)
    encrypt.UseCipherNamed("billing", billingCipher)

    /* a column bound to a compartment round-trips through that compartment's cipher */
    plaintext := "RO49 AAAA 1B31 0075 9384 0000"

    crmColumn := encrypt.EncryptedStringFor[crmCipherRef](plaintext)

    ciphertext := driverValueString(crmColumn.Value())
    if true == strings.Contains(ciphertext, plaintext) {
        fail("encrypt: the ciphertext contains the plaintext")
    }

    var restored encrypt.EncryptedStringFor[crmCipherRef]
    if scanErr := restored.Scan(ciphertext); nil != scanErr {
        fail("encrypt: crm column Scan: %v", scanErr)
    }
    if plaintext != string(restored) {
        fail("encrypt: crm round-trip mismatch — wanted %q, got %q", plaintext, string(restored))
    }
    pass("encrypt compartment round-trip through its own cipher")

    /* the other compartment must not be able to read it */
    var crossColumn encrypt.EncryptedStringFor[billingCipherRef]
    crossErr := crossColumn.Scan(ciphertext)
    if nil == crossErr {
        fail("encrypt: the billing compartment decrypted a crm ciphertext (compartments are not isolated)")
    }
    if plaintext == string(crossColumn) {
        fail("encrypt: the billing compartment recovered the crm plaintext")
    }
    pass("encrypt compartments are isolated (billing cannot read crm)")

    /* and the ciphertexts of the two compartments differ for identical plaintext */
    billingColumn := encrypt.EncryptedStringFor[billingCipherRef](plaintext)

    if ciphertext == driverValueString(billingColumn.Value()) {
        fail("encrypt: both compartments produced the same ciphertext for the same plaintext")
    }
    pass("encrypt compartments produce distinct ciphertexts for the same plaintext")

    /* the deterministic variant is searchable inside a compartment and still isolated across them */
    firstDeterministic := encrypt.EncryptedDeterministicStringFor[crmCipherRef](plaintext)
    secondDeterministic := encrypt.EncryptedDeterministicStringFor[crmCipherRef](plaintext)

    firstStored := driverValueString(firstDeterministic.Value())
    secondStored := driverValueString(secondDeterministic.Value())
    if firstStored != secondStored {
        fail("encrypt: the deterministic cipher produced two different ciphertexts for the same plaintext")
    }

    crossDeterministic := encrypt.EncryptedDeterministicStringFor[billingCipherRef](plaintext)
    if firstStored == driverValueString(crossDeterministic.Value()) {
        fail("encrypt: the deterministic ciphertext is equal across compartments (equality leaks across keys)")
    }
    pass("encrypt deterministic values are searchable inside a compartment and differ across compartments")

    /* the plaintext must never leak through a rendering path; %#v is the verb that reaches for GoStringer, and the one that leaked */
    if true == strings.Contains(fmt.Sprintf("%v %s %+v %#v", crmColumn, crmColumn, crmColumn, crmColumn), plaintext) {
        fail("encrypt: the plaintext leaked through a formatting verb")
    }
    if true == strings.Contains(fmt.Sprintf("%#v %v", firstDeterministic, firstDeterministic), plaintext) {
        fail("encrypt: the plaintext leaked through a formatting verb on the deterministic column")
    }

    encoded, marshalErr := json.Marshal(map[string]any{"iban": crmColumn})
    if nil != marshalErr {
        fail("encrypt: marshal: %v", marshalErr)
    }
    if true == strings.Contains(string(encoded), plaintext) {
        fail("encrypt: the plaintext leaked through JSON marshalling")
    }
    if true == strings.Contains(crmColumn.LogValue().String(), plaintext) {
        fail("encrypt: the plaintext leaked through the log value")
    }
    pass("encrypt redacts the plaintext in string, format, log and JSON renderings")

    /* an unregistered compartment errors rather than silently falling back to the default cipher */
    unregistered := encrypt.EncryptedStringFor[unregisteredCipherRef](plaintext)

    if _, unregisteredErr := unregistered.Value(); nil == unregisteredErr {
        fail("encrypt: an unregistered compartment encrypted with a fallback cipher")
    }
    pass("encrypt refuses to encrypt for an unregistered compartment")
}

/* driverValueString renders what a column hands the driver, whichever concrete type it chose: the encrypted
columns store their ciphertext as bytes, and a Stringer-free []byte would otherwise have to be asserted at
every call site. */
func driverValueString(value driver.Value, valueErr error) string {
    if nil != valueErr {
        fail("encrypt: column Value: %v", valueErr)
    }

    switch typed := value.(type) {
    case string:
        return typed
    case []byte:
        return string(typed)
    }

    fail("encrypt: column Value returned %T, wanted a string or a byte slice", value)

    return ""
}
