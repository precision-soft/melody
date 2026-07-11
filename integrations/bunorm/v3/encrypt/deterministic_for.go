package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "log/slog"
)

/* EncryptedDeterministicStringFor is EncryptedDeterministicString bound to the named cipher selected by the CipherRef marker R — the searchable (equality-preserving) variant of EncryptedStringFor; see that type for the compartment semantics. */
type EncryptedDeterministicStringFor[R CipherRef] string

func (instance EncryptedDeterministicStringFor[R]) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedDeterministicStringFor[R]) GoString() string {
    return redactedPlaceholder
}

func (instance EncryptedDeterministicStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedDeterministicStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
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
