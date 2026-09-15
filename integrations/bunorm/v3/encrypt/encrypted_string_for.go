package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
)

/* EncryptedStringFor binds EncryptedString to the named cipher selected by the zero-value CipherRef marker R. Install it with UseCipherNamed; use distinct keys to isolate compartments. */
type EncryptedStringFor[R CipherRef] string

func (instance EncryptedStringFor[R]) encryptedColumn() {}

func (instance EncryptedStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString returns the redacted representation. */
func (instance EncryptedStringFor[R]) GoString() string {
    return redactedPlaceholder
}

/* Format redacts values when fmt invokes the Formatter interface. */
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

func refCipher[R CipherRef]() (Cipher, error) {
    var ref R

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
