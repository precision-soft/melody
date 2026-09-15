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

    gcmTagOverhead = 16
)

var markerPrefix = encryptionMarker + markerGlue + formatGcmV1 + markerGlue

type Cipher interface {
    Encrypt(plaintext string) (string, error)

    EncryptWithKeyId(plaintext string, keyId string) (string, error)

    EncryptDeterministic(plaintext string) (string, error)

    EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error)

    CiphertextCandidates(plaintext string) ([][]byte, error)

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
    if true == instance.isPassThroughCiphertext(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, instance.keys.CurrentKeyId(), false)
}

func (instance *aes256Cipher) EncryptWithKeyId(plaintext string, keyId string) (string, error) {
    if true == instance.isPassThroughCiphertext(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, keyId, false)
}

func (instance *aes256Cipher) EncryptDeterministic(plaintext string) (string, error) {
    return instance.sealDeterministic(plaintext, instance.keys.CurrentKeyId())
}

func (instance *aes256Cipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    return instance.sealDeterministic(plaintext, keyId)
}

func (instance *aes256Cipher) sealDeterministic(plaintext string, keyId string) (string, error) {
    opened, sealed := instance.authenticatedSeal(plaintext)
    if false == sealed {
        return instance.seal(plaintext, keyId, true)
    }

    if true == opened.deterministic {
        return plaintext, nil
    }

    return instance.seal(opened.plaintext, opened.keyId, true)
}

type openedSeal struct {
    plaintext     string
    keyId         string
    deterministic bool
}

func (instance *aes256Cipher) authenticatedSeal(value string) (openedSeal, bool) {
    if false == hasEncryptionMarker(value) {
        return openedSeal{}, false
    }

    keyId, payload, decodeErr := decodeEncrypted(value)
    if nil != decodeErr {
        return openedSeal{}, false
    }

    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return openedSeal{}, false
    }

    gcm, gcmErr := gcmForKey(key, keyId)
    if nil != gcmErr {
        return openedSeal{}, false
    }

    nonce := payload[:gcm.NonceSize()]

    plaintext, openErr := gcm.Open(nil, nonce, payload[gcm.NonceSize():], nil)
    if nil != openErr {
        return openedSeal{}, false
    }

    return openedSeal{
        plaintext:     string(plaintext),
        keyId:         keyId,
        deterministic: hmac.Equal(nonce, deterministicNonce(key, string(plaintext), gcm.NonceSize())),
    }, true
}

func (instance *aes256Cipher) CiphertextCandidates(plaintext string) ([][]byte, error) {
    keyIds := instance.keys.ActiveKeyIds()

    candidates := make([][]byte, 0, len(keyIds))
    for _, keyId := range keyIds {
        candidate, sealErr := instance.seal(plaintext, keyId, true)
        if nil != sealErr {
            return nil, sealErr
        }

        candidates = append(candidates, []byte(candidate))
    }

    return candidates, nil
}

/* Decrypt passes unmarked plaintext through for incremental migration. Marked values must parse and authenticate under a known key; malformed, truncated or unauthentic ciphertext returns an error. */
func (instance *aes256Cipher) Decrypt(encoded string) (string, error) {
    if false == hasEncryptionMarker(encoded) {
        return encoded, nil
    }

    keyId, payload, decodeErr := decodeEncrypted(encoded)
    if nil != decodeErr {
        return "", decodeErr
    }

    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return "", keyErr
    }

    gcm, gcmErr := gcmForKey(key, keyId)
    if nil != gcmErr {
        return "", gcmErr
    }

    nonce := payload[:gcm.NonceSize()]
    ciphertext := payload[gcm.NonceSize():]

    plaintext, openErr := gcm.Open(nil, nonce, ciphertext, nil)
    if nil != openErr {
        return "", exception.NewError("could not decrypt value", map[string]any{"keyId": keyId}, openErr)
    }

    return string(plaintext), nil
}

func (instance *aes256Cipher) isPassThroughCiphertext(value string) bool {
    if false == hasEncryptionMarker(value) {
        return false
    }

    _, decryptErr := instance.Decrypt(value)

    return nil == decryptErr
}

func (instance *aes256Cipher) seal(plaintext string, keyId string, deterministic bool) (string, error) {

    if false == keyIdPattern.MatchString(keyId) {
        return "", exception.NewError("encryption key id must match "+keyIdPattern.String(), map[string]any{"keyId": keyId}, nil)
    }

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

func keyIdOf(encoded string) (string, bool, error) {
    if false == hasEncryptionMarker(encoded) {
        return "", false, nil
    }

    keyId, _, decodeErr := decodeEncrypted(encoded)
    if nil != decodeErr {
        return "", false, decodeErr
    }

    return keyId, true, nil
}

func deterministicNonce(key []byte, plaintext string, size int) []byte {
    subKeyMac := hmac.New(sha256.New, key)
    subKeyMac.Write([]byte(deterministicNonceLabel))
    nonceKey := subKeyMac.Sum(nil)

    nonceMac := hmac.New(sha256.New, nonceKey)
    nonceMac.Write([]byte(plaintext))

    return nonceMac.Sum(nil)[:size]
}

func hasEncryptionMarker(value string) bool {
    return strings.HasPrefix(value, markerPrefix)
}

func decodeEncrypted(value string) (string, []byte, error) {
    body := value[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator || 0 == separator {
        return "", nil, exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := body[:separator]

    payload, decodeErr := base64.RawStdEncoding.Strict().DecodeString(body[separator+1:])
    if nil != decodeErr {
        return "", nil, exception.NewError("encrypted value is not valid base64", map[string]any{"keyId": keyId}, decodeErr)
    }

    if len(payload) < minNonceSize+gcmTagOverhead {
        return "", nil, exception.NewError(
            "encrypted value is too short",
            map[string]any{"keyId": keyId, "payloadBytes": len(payload)},
            nil,
        )
    }

    return keyId, payload, nil
}

var _ Cipher = (*aes256Cipher)(nil)
