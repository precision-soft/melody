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

Install the compartment with encrypt.UseCipherNamed("crm", cipher). Each named cipher owns its KeyProvider: key rotation inside the compartment keeps working through the key id embedded in the ciphertext, and a column of one compartment can never decrypt (or be decrypted by) another's. */
type EncryptedStringFor[R CipherRef] string

func (instance EncryptedStringFor[R]) encryptedColumn() {}

func (instance EncryptedStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedStringFor[R]) GoString() string {
    return redactedPlaceholder
}

func (instance EncryptedStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
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

/* refCipher resolves the named cipher a marker type selects; the marker is instantiated as its zero value, which is why CipherRef implementations should be zero-size value-receiver types. A marker whose CipherName() is empty is rejected rather than resolved: the empty name is the default cipher's reserved entry, so accepting it would quietly hand a compartment-bound column the default key — the exact cross-compartment read the marker exists to prevent. */
func refCipher[R CipherRef]() (Cipher, error) {
    var ref R

    /* a pointer-form marker (EncryptedStringFor[*CrmCipher]) compiles whenever the value form does, and its zero value is a nil pointer whose CipherName() call dereferences nil — a panic raised from inside database/sql on the first column read or write. It is answered as an error naming the marker instead. */
    if reflected := reflect.ValueOf(any(ref)); reflect.Ptr == reflected.Kind() && true == reflected.IsNil() {
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
