package encrypt

import (
    "database/sql/driver"
    "log/slog"
)

/**
 * EncryptedDeterministicString is a searchable encrypted column: it encrypts with a
 * plaintext-derived nonce, so equal plaintext produces equal ciphertext under the same key and
 * the column can be queried with equality / `IN (...)` predicates. Build the right-hand values
 * with Cipher.CiphertextCandidates so lookups survive key rotation. Because it reveals plaintext
 * equality, use it only on low-entropy lookup fields (e.g. an email used solely to find a row),
 * never on secrets where equality must stay hidden — use EncryptedString there.
 */
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

    return encoded, nil
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
