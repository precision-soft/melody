package encrypt

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

const (
    encryptionMarker = "<ENC>"
    markerGlue       = "\x00"
    formatGcmV1      = "gcm1"

    deterministicNonceLabel = "melody/encrypt/deterministic-nonce/v1"

    minNonceSize = 12
)

var markerPrefix = encryptionMarker + markerGlue + formatGcmV1 + markerGlue

type Cipher interface {
    Encrypt(plaintext string) (string, error)

    EncryptWithKeyId(plaintext string, keyId string) (string, error)

    EncryptDeterministic(plaintext string) (string, error)

    EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error)

    CiphertextCandidates(plaintext string) ([]string, error)

    Decrypt(encoded string) (string, error)
}

func NewCipher(keys KeyProvider) Cipher {
    if nil == keys {
        exception.Panic(exception.NewError("cipher key provider is nil", nil, nil))
    }

    return &aes256Cipher{
        keys: keys,
    }
}

type aes256Cipher struct {
    keys KeyProvider
}

func (instance *aes256Cipher) Encrypt(plaintext string) (string, error) {
    if true == looksEncrypted(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, instance.keys.CurrentKeyId(), false)
}

func (instance *aes256Cipher) EncryptWithKeyId(plaintext string, keyId string) (string, error) {
    if true == looksEncrypted(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, keyId, false)
}

func (instance *aes256Cipher) EncryptDeterministic(plaintext string) (string, error) {
    if true == looksEncrypted(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, instance.keys.CurrentKeyId(), true)
}

func (instance *aes256Cipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    if true == looksEncrypted(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, keyId, true)
}

func (instance *aes256Cipher) CiphertextCandidates(plaintext string) ([]string, error) {
    keyIds := instance.keys.ActiveKeyIds()

    candidates := make([]string, 0, len(keyIds))
    for _, keyId := range keyIds {
        candidate, sealErr := instance.seal(plaintext, keyId, true)
        if nil != sealErr {
            return nil, sealErr
        }

        candidates = append(candidates, candidate)
    }

    return candidates, nil
}

func (instance *aes256Cipher) seal(plaintext string, keyId string, deterministic bool) (string, error) {
    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return "", keyErr
    }

    gcm, gcmErr := gcmForKey(key, keyId)
    if nil != gcmErr {
        return "", gcmErr
    }

    var nonce []byte
    if true == deterministic {
        nonce = deterministicNonce(key, plaintext, gcm.NonceSize())
    } else {
        nonce = make([]byte, gcm.NonceSize())
        if _, readErr := rand.Read(nonce); nil != readErr {
            return "", exception.NewError("could not generate a nonce", nil, readErr)
        }
    }

    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

    return markerPrefix + keyId + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (instance *aes256Cipher) Decrypt(encoded string) (string, error) {
    if false == looksEncrypted(encoded) {
        return encoded, nil
    }

    body := encoded[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator {
        return "", exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := body[:separator]

    payload, decodeErr := base64.RawStdEncoding.DecodeString(body[separator+1:])
    if nil != decodeErr {
        return "", exception.NewError("encrypted value is not valid base64", nil, decodeErr)
    }

    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return "", keyErr
    }

    gcm, gcmErr := gcmForKey(key, keyId)
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

func gcmForKey(key []byte, keyId string) (cipher.AEAD, error) {
    block, blockErr := aes.NewCipher(key)
    if nil != blockErr {
        return nil, exception.NewError("invalid encryption key", map[string]any{"keyId": keyId}, blockErr)
    }

    gcm, gcmErr := cipher.NewGCM(block)
    if nil != gcmErr {
        return nil, exception.NewError("could not create gcm", map[string]any{"keyId": keyId}, gcmErr)
    }

    if minNonceSize != gcm.NonceSize() {
        return nil, exception.NewError(
            "unexpected gcm nonce size",
            map[string]any{"keyId": keyId, "nonceSize": gcm.NonceSize()},
            nil,
        )
    }

    return gcm, nil
}

func keyIdOf(encoded string) (string, bool) {
    if false == looksEncrypted(encoded) {
        return "", false
    }

    body := encoded[len(markerPrefix):]
    separator := strings.IndexByte(body, ':')

    return body[:separator], true
}

func deterministicNonce(key []byte, plaintext string, size int) []byte {
    subKeyMac := hmac.New(sha256.New, key)
    subKeyMac.Write([]byte(deterministicNonceLabel))
    nonceKey := subKeyMac.Sum(nil)

    nonceMac := hmac.New(sha256.New, nonceKey)
    nonceMac.Write([]byte(plaintext))

    return nonceMac.Sum(nil)[:size]
}

func looksEncrypted(value string) bool {
    if false == strings.HasPrefix(value, markerPrefix) {
        return false
    }

    body := value[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator || 0 == separator {
        return false
    }

    payload, decodeErr := base64.RawStdEncoding.DecodeString(body[separator+1:])
    if nil != decodeErr {
        return false
    }

    return len(payload) >= minNonceSize
}
