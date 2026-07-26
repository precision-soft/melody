package encrypt

import (
    "sync/atomic"
)

var fakeCipherSequenceNumber atomic.Uint64

func NewFakeCipher() Cipher {
    return &fakeCipher{
        instanceNumber: fakeCipherSequenceNumber.Add(1),
    }
}

/* the instance number is what keeps two fakes apart: a zero-size struct puts every instance at the same address, so two fakes installed in different registry compartments would compare EQUAL and any assertion that they stayed apart would hold whichever compartment the registry answered from. */
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
