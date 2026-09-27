package encrypt

import (
    "sync/atomic"
)

var fakeCipherSequenceNumber atomic.Uint64

/* NewFakeCipher answers a cipher whose every operation is the identity, for tests and local development. In production it writes plaintext into every encrypted column with no error, where having no cipher fails closed at the first write, so no production wiring path may reach it. */
func NewFakeCipher() Cipher {
    return &fakeCipher{
        instanceNumber: fakeCipherSequenceNumber.Add(1),
    }
}

/* the instance number keeps two fakes apart: a zero-size struct gives every instance the same address, so two fakes would compare equal */
type fakeCipher struct {
    instanceNumber uint64
}

func (instance *fakeCipher) Encrypt(plaintext string) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) EncryptWithKeyId(plaintext string, keyId string) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) EncryptDeterministic(plaintext string) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) CiphertextCandidates(plaintext string) ([][]byte, error) {
    return [][]byte{[]byte(plaintext)}, nil
}

func (instance *fakeCipher) Decrypt(encoded string) (string, error) {
    return encoded, nil
}

var _ Cipher = (*fakeCipher)(nil)
