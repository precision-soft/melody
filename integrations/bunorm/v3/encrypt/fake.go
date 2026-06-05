package encrypt

/**
 * NewFakeCipher returns an identity Cipher that stores and returns values unchanged. It is for
 * tests and local development only — it provides NO confidentiality. Never install it in production.
 */
func NewFakeCipher() Cipher {
    return &fakeCipher{}
}

type fakeCipher struct{}

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

func (instance *fakeCipher) CiphertextCandidates(plaintext string) ([]string, error) {
    return []string{plaintext}, nil
}

func (instance *fakeCipher) Decrypt(encoded string) (string, error) {
    return encoded, nil
}

var _ Cipher = (*fakeCipher)(nil)
