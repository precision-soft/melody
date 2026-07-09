package encrypt

import (
    "database/sql/driver"
    "encoding/json"
    "log/slog"
)

/* EncryptedStringFor is EncryptedString bound to the named cipher selected by the CipherRef marker R, so a multi-context binary keeps one key compartment per context:

    type CrmCipher struct{}

    func (instance CrmCipher) CipherName() string {
        return "crm"
    }

    type Customer struct {
        Iban encrypt.EncryptedStringFor[CrmCipher] `bun:"iban"`
    }

Install the compartment with encrypt.UseCipherNamed("crm", cipher). Each named cipher owns its KeyProvider: key rotation inside the compartment keeps working through the key id embedded in the ciphertext, and a column of one compartment can never decrypt (or be decrypted by) another's. */
type EncryptedStringFor[R CipherRef] string

func (instance EncryptedStringFor[R]) String() string {
    return redactedPlaceholder
}

func (instance EncryptedStringFor[R]) LogValue() slog.Value {
    return slog.StringValue(redactedPlaceholder)
}

func (instance EncryptedStringFor[R]) MarshalJSON() ([]byte, error) {
    return json.Marshal(redactedPlaceholder)
}

func (instance EncryptedStringFor[R]) Value() (driver.Value, error) {
    cipherInstance, cipherErr := refCipher[R]()
    if nil != cipherErr {
        return nil, cipherErr
    }

    encoded, encryptErr := cipherInstance.Encrypt(string(instance))
    if nil != encryptErr {
        return nil, encryptErr
    }

    return []byte(encoded), nil
}

func (instance *EncryptedStringFor[R]) Scan(source any) error {
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

    *instance = EncryptedStringFor[R](plaintext)

    return nil
}

/* refCipher resolves the named cipher a marker type selects; the marker is instantiated as its zero value, which is why CipherRef implementations should be zero-size value-receiver types. */
func refCipher[R CipherRef]() (Cipher, error) {
    var ref R

    return cipherByName(ref.CipherName())
}
