package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "log/slog"

    "github.com/precision-soft/melody/v3/exception"
)

const redactedPlaceholder = "<redacted>"

/* EncryptedColumn marks every encrypted column type — the two default-cipher forms and every instantiation of the two compartment-bound generic forms. A consumer that must recognise "a value of an encrypted column type" (the audit trail's auto-redaction) matches this interface rather than comparing types by identity, because each generic instantiation is a distinct reflect.Type and an identity list could never enumerate them. */
type EncryptedColumn interface {
    encryptedColumn()
}

type EncryptedString string

func (instance EncryptedString) encryptedColumn() {}

func (instance EncryptedString) String() string {
    return redactedPlaceholder
}

/* GoString redacts under the %#v verb too: fmt reaches for GoStringer there and would otherwise print the underlying string literal, so a struct dumped with %#v in a log line or a test failure would carry the plaintext. */
func (instance EncryptedString) GoString() string {
    return redactedPlaceholder
}

func (instance EncryptedString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

func (instance EncryptedString) Value() (driver.Value, error) {
    cipherInstance, cipherErr := cipherByName(defaultCipherName)
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.Encrypt(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedString) Scan(source any) error {
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

    *instance = EncryptedString(plaintext)

    return nil
}

func scanRaw(source any) (string, bool, error) {
    if nil == source {
        return "", true, nil
    }

    switch typed := source.(type) {
    case string:
        return typed, false, nil
    case []byte:
        return string(typed), false, nil
    default:
        return "", false, exception.NewError("encrypted string scan received an unsupported type", nil, nil)
    }
}

func errCipherNotConfigured() error {
    return exception.NewError("encryption cipher is not configured; call encrypt.UseCipher(...) first", nil, nil)
}

var _ driver.Valuer = EncryptedString("")
var _ EncryptedColumn = EncryptedString("")
