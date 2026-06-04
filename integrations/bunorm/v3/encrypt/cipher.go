package encrypt

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

func NewCipher(keys KeyProvider) *Cipher {
    if nil == keys {
        exception.Panic(exception.NewError("cipher key provider is nil", nil, nil))
    }

    return &Cipher{
        keys: keys,
    }
}

type Cipher struct {
    keys KeyProvider
}

func (instance *Cipher) Encrypt(plaintext string) (string, error) {
    keyId := instance.keys.CurrentKeyId()

    gcm, gcmErr := instance.gcmFor(keyId)
    if nil != gcmErr {
        return "", gcmErr
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, readErr := rand.Read(nonce); nil != readErr {
        return "", exception.NewError("could not generate a nonce", nil, readErr)
    }

    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

    return keyId + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (instance *Cipher) Decrypt(encoded string) (string, error) {
    separator := strings.IndexByte(encoded, ':')
    if -1 == separator {
        return "", exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := encoded[:separator]

    payload, decodeErr := base64.RawStdEncoding.DecodeString(encoded[separator+1:])
    if nil != decodeErr {
        return "", exception.NewError("encrypted value is not valid base64", nil, decodeErr)
    }

    gcm, gcmErr := instance.gcmFor(keyId)
    if nil != gcmErr {
        return "", gcmErr
    }

    if len(payload) < gcm.NonceSize() {
        return "", exception.NewError("encrypted value is too short", nil, nil)
    }

    nonce := payload[:gcm.NonceSize()]
    ciphertext := payload[gcm.NonceSize():]

    plaintext, openErr := gcm.Open(nil, nonce, ciphertext, nil)
    if nil != openErr {
        return "", exception.NewError("could not decrypt value", map[string]any{"keyId": keyId}, openErr)
    }

    return string(plaintext), nil
}

func (instance *Cipher) gcmFor(keyId string) (cipher.AEAD, error) {
    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return nil, keyErr
    }

    block, blockErr := aes.NewCipher(key)
    if nil != blockErr {
        return nil, exception.NewError("invalid encryption key", map[string]any{"keyId": keyId}, blockErr)
    }

    gcm, gcmErr := cipher.NewGCM(block)
    if nil != gcmErr {
        return nil, exception.NewError("could not create gcm", map[string]any{"keyId": keyId}, gcmErr)
    }

    return gcm, nil
}
