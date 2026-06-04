package encrypt

import (
    "database/sql/driver"

    "github.com/precision-soft/melody/v3/exception"
)

var packageCipher *Cipher

func UseCipher(cipherInstance *Cipher) {
    packageCipher = cipherInstance
}

type EncryptedString string

func (instance EncryptedString) Value() (driver.Value, error) {
    if nil == packageCipher {
        return string(instance), nil
    }

    encoded, encryptErr := packageCipher.Encrypt(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return encoded, nil
}

func (instance *EncryptedString) Scan(source any) error {
    if nil == source {
        *instance = ""
        return nil
    }

    raw := ""
    switch typed := source.(type) {
    case string:
        raw = typed
    case []byte:
        raw = string(typed)
    default:
        return exception.NewError("encrypted string scan received an unsupported type", nil, nil)
    }

    if nil == packageCipher {
        *instance = EncryptedString(raw)
        return nil
    }

    plaintext, decryptErr := packageCipher.Decrypt(raw)
    if nil != decryptErr {
        return decryptErr
    }

    *instance = EncryptedString(plaintext)

    return nil
}

var _ driver.Valuer = EncryptedString("")
