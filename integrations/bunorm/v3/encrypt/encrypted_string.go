package encrypt

import (
    "database/sql/driver"
    "log/slog"

    "github.com/precision-soft/melody/v3/exception"
)

const redactedPlaceholder = "<redacted>"

var packageCipher Cipher

func UseCipher(cipherInstance Cipher) {
    packageCipher = cipherInstance
}

type EncryptedString string

func (instance EncryptedString) String() string {
    return redactedPlaceholder
}

func (instance EncryptedString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedString) Value() (driver.Value, error) {
    if nil == packageCipher {
        return nil, errCipherNotConfigured()
    }

    encoded, encryptErr := packageCipher.Encrypt(string(instance))
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

    if nil == packageCipher {
        return errCipherNotConfigured()
    }

    plaintext, plaintextErr := packageCipher.Decrypt(raw)
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
