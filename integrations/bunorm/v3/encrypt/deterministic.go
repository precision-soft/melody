package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "log/slog"
)

/* EncryptedDeterministicString is EncryptedString with the nonce derived from the plaintext, so equal plaintext seals to equal ciphertext and the column answers an equality lookup through Cipher.CiphertextCandidates. The nonce is keyed by the key and the plaintext alone, so equal values are correlatable across every deterministic column and table sealed under the same key; the readme's "Searchable (deterministic) encryption" section says what that rules the type out for. */
type EncryptedDeterministicString string

func (instance EncryptedDeterministicString) encryptedColumn() {}

func (instance EncryptedDeterministicString) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedDeterministicString) GoString() string {
    return redactedPlaceholder
}

/* Format answers every verb that reaches it, the numeric ones included, with the redacted rendering; its limits are on EncryptedString.Format. */
func (instance EncryptedDeterministicString) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(redactedPlaceholder))
}

func (instance EncryptedDeterministicString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedDeterministicString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

/* UnmarshalJSON refuses the redaction placeholder MarshalJSON writes and decodes any other string, for the reason on EncryptedString.UnmarshalJSON. */
func (instance *EncryptedDeterministicString) UnmarshalJSON(data []byte) error {
    return unmarshalEncryptedJson(instance, data)
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
