package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
)

/* EncryptedDeterministicStringFor is EncryptedDeterministicString bound to the named cipher the CipherRef marker R selects; see EncryptedStringFor for the compartment semantics and EncryptedDeterministicString for what the plaintext-derived nonce reveals. */
type EncryptedDeterministicStringFor[R CipherRef] string

func (instance EncryptedDeterministicStringFor[R]) encryptedColumn() {}

func (instance EncryptedDeterministicStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedDeterministicStringFor[R]) GoString() string {
    return redactedPlaceholder
}

/* Format answers every verb that reaches it, the numeric ones included, with the redacted rendering; its limits are on EncryptedString.Format. */
func (instance EncryptedDeterministicStringFor[R]) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedDeterministicStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedDeterministicStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes and decodes any other string, for the reason on EncryptedString.UnmarshalJSON. */
func (instance *EncryptedDeterministicStringFor[R]) UnmarshalJSON(data []byte) error {
    return unmarshalEncryptedJson(instance, data)
}

func (instance EncryptedDeterministicStringFor[R]) Value() (driver.Value, error) {
    cipherInstance, cipherErr := refCipher[R]()
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.EncryptDeterministic(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedDeterministicStringFor[R]) Scan(source any) error {
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

    *instance = EncryptedDeterministicStringFor[R](plaintext)

    return nil
}
