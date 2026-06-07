package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "log/slog"
)

type EncryptedDeterministicString string

func (instance EncryptedDeterministicString) String() string {
    return redactedPlaceholder
}

func (instance EncryptedDeterministicString) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

/** MarshalJSON redacts the plaintext so an encrypted value never leaks through JSON encoding, including when it is nested inside another value (a named struct field, slice, map, or array) that the audit recorder serializes into its changes column. The encrypted form for storage is produced by Value, not by JSON. */
func (instance EncryptedDeterministicString) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
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
