package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
)

/* EncryptedDeterministicString is EncryptedString with the nonce derived from the plaintext, so equal plaintext seals to equal ciphertext and the column answers an equality lookup through Cipher.CiphertextCandidates. The nonce is keyed by the key and the plaintext alone — nothing names the column or the table — so the same plaintext is byte-identical across every deterministic column and table sealed under the same key, and an observer of the stored values can correlate equal values across all of them; the readme's "Searchable (deterministic) encryption" section says what that rules the type out for. Binding the nonce to a column would be a new wire format and is the next major's. */
type EncryptedDeterministicString string

func (instance EncryptedDeterministicString) encryptedColumn() {}

func (instance EncryptedDeterministicString) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedDeterministicString) GoString() string {
    return redactedPlaceholder
}

func (instance EncryptedDeterministicString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedDeterministicString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes and decodes any other string, for the reason on EncryptedString.UnmarshalJSON. */
func (instance *EncryptedDeterministicString) UnmarshalJSON(data []byte) error {
    decoded, present, decodeErr := decodeEncryptedJson(data, fmt.Sprintf("%T", *instance))
    if nil != decodeErr {
        return decodeErr
    }

    if true == present {
        *instance = EncryptedDeterministicString(decoded)
    }

    return nil
}

func (instance EncryptedDeterministicString) Value() (driver.Value, error) {
    cipherInstance, cipherErr := cipherByName(defaultCipherName)
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.EncryptDeterministic(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedDeterministicString) Scan(source any) error {
    raw, isNull, decodeErr := scanRaw(source)
    if nil != decodeErr {
        return decodeErr
    }

    if true == isNull {
        *instance = ""
        return nil
    }

    cipherInstance, cipherErr := cipherByName(defaultCipherName)
    if nil != cipherErr {
        return cipherErr
    }

    plaintext, plaintextErr := cipherInstance.Decrypt(raw)
    if nil != plaintextErr {
        return plaintextErr
    }

    *instance = EncryptedDeterministicString(plaintext)

    return nil
}

var _ driver.Valuer = EncryptedDeterministicString("")
var _ json.Unmarshaler = (*EncryptedDeterministicString)(nil)
var _ EncryptedColumn = EncryptedDeterministicString("")
