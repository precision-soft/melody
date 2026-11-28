package encrypt

import (
    "sync/atomic"
)

var fakeCipherSequenceNumber atomic.Uint64

const (
    fakeCipherConstructorName           = "encrypt.NewFakeCipher"
    fakeCipherOutsideDevelopmentMessage = "the encrypt module is handed the fake cipher outside the development environment: it would write every encrypted column in clear"
)

/* NewFakeCipher answers a cipher whose every operation is the identity, for tests and local development. In production it writes plaintext into every encrypted column with no error, where having no cipher fails closed at the first write, so the module refuses it at registration outside the development environment, naming the field. A bound column over it stores the plaintext as well, and its binding and the binding's UnboundAdmitted mean nothing, since there is no format to read. */
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

/* the fake carries the binding door as the identity: a bound column over it stores the plaintext and reads back what it holds, and a binding's switch has no format to read */
func (instance *fakeCipher) sealBoundValue(plaintext string, binding sealBinding, deterministic bool) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) sealBoundValueWithKeyId(plaintext string, keyId string, binding sealBinding, deterministic bool) (string, error) {
    return plaintext, nil
}

func (instance *fakeCipher) openStored(stored string, binding sealBinding) (openedSeal, error) {
    return openedSeal{plaintext: stored}, nil
}

func (instance *fakeCipher) boundCandidates(plaintext string, binding sealBinding) ([][]byte, error) {
    return [][]byte{[]byte(plaintext)}, nil
}

func isFakeCipher(cipher Cipher) bool {
    _, isFake := cipher.(*fakeCipher)

    return true == isFake
}

var _ Cipher = (*fakeCipher)(nil)

var _ bindingSealer = (*fakeCipher)(nil)
