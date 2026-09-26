package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
)

/* EncryptedStringFor is EncryptedString bound to the named cipher selected by the CipherRef marker R, so a multi-context binary keeps one key compartment per context:

    type CrmCipher struct{}

    func (instance CrmCipher) CipherName() string {
        return "crm"
    }

    type Customer struct {
        Iban encrypt.EncryptedStringFor[CrmCipher] `bun:"iban"`
    }

Install the compartment with encrypt.UseCipherNamed("crm", cipher). Each named cipher owns its KeyProvider: key rotation inside the compartment keeps working through the key id embedded in the ciphertext, and a column of one compartment cannot decrypt another's as long as each provider holds keys of its own, since the name is not bound into the ciphertext. */
type EncryptedStringFor[R CipherRef] string

func (instance EncryptedStringFor[R]) encryptedColumn() {}

func (instance EncryptedStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedStringFor[R]) GoString() string {
    return redactedPlaceholder
}

/* Format answers every verb that reaches it, the numeric ones included, with the redacted rendering; its limits are on EncryptedString.Format. */
func (instance EncryptedStringFor[R]) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes and decodes any other string, for the reason on EncryptedString.UnmarshalJSON. */
func (instance *EncryptedStringFor[R]) UnmarshalJSON(data []byte) error {
    return unmarshalEncryptedJson(instance, data)
}

func (instance EncryptedStringFor[R]) Value() (driver.Value, error) {
    cipherInstance, cipherErr := refCipher[R]()
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.Encrypt(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedStringFor[R]) Scan(source any) error {
    raw, isNull, decodeErr := scanRaw(source)
    if nil != decodeErr {
        return decodeErr
    }

    if true == isNull {
        *instance = ""
        return nil
    }

    cipherInstance, cipherErr := refCipher[R]()
    if nil != cipherErr {
        return cipherErr
    }

    plaintext, plaintextErr := cipherInstance.Decrypt(raw)
    if nil != plaintextErr {
        return plaintextErr
    }

    *instance = EncryptedStringFor[R](plaintext)

    return nil
}

/* refCipher resolves the cipher a marker type selects, instantiating the marker as its zero value. An empty CipherName is refused, since the empty name is the default cipher's entry and would hand a compartment-bound column the default key. */
func refCipher[R CipherRef]() (Cipher, error) {
    var ref R

    /* a pointer-form marker's zero value is nil, so its CipherName call would panic inside database/sql; it is answered as an error naming the marker */
    if reflected := reflect.ValueOf(any(ref)); reflect.Pointer == reflected.Kind() && true == reflected.IsNil() {
        return nil, exception.NewError(
            "cipher reference is a pointer type; a CipherRef must be a zero-size value type",
            map[string]any{"cipherRef": fmt.Sprintf("%T", ref)},
            nil,
        )
    }

    name := ref.CipherName()
    if "" == name {
        return nil, exception.NewError(
            "cipher reference name is empty; a CipherRef must name a compartment registered with encrypt.UseCipherNamed(...)",
            map[string]any{"cipherRef": fmt.Sprintf("%T", ref)},
            nil,
        )
    }

    return cipherByName(name)
}
