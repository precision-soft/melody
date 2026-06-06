package encrypt

import (
    "database/sql/driver"
    "log/slog"
)

type EncryptedDeterministicString string

func (instance EncryptedDeterministicString) String() string {
    return redactedPlaceholder
}

func (instance EncryptedDeterministicString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedDeterministicString) Value() (driver.Value, error) {
    if nil == packageCipher {
        return nil, errCipherNotConfigured()
    }

    encoded, encryptErr := packageCipher.EncryptDeterministic(string(instance))
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

    if nil == packageCipher {
        return errCipherNotConfigured()
    }

    plaintext, plaintextErr := packageCipher.Decrypt(raw)
    if nil != plaintextErr {
        return plaintextErr
    }

    *instance = EncryptedDeterministicString(plaintext)

    return nil
}

var _ driver.Valuer = EncryptedDeterministicString("")
