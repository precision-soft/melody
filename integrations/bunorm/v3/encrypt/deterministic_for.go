package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
)

/* EncryptedDeterministicStringFor is EncryptedDeterministicString bound to the named cipher selected by the CipherRef marker R — the searchable (equality-preserving) variant of EncryptedStringFor; see that type for the compartment semantics, and EncryptedDeterministicString for what the plaintext-derived nonce reveals: equal plaintext is byte-identical across every deterministic column and table sealed under the same key. */
type EncryptedDeterministicStringFor[R CipherRef] string

func (instance EncryptedDeterministicStringFor[R]) encryptedColumn() {}

func (instance EncryptedDeterministicStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString returns the redacted representation. */
func (instance EncryptedDeterministicStringFor[R]) GoString() string {
    return redactedPlaceholder
}

/* Format redacts values when fmt invokes the Formatter interface. */
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
