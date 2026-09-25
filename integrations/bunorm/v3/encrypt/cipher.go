package encrypt

import (
    "bytes"
    "crypto/aes"
    "crypto/cipher"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "strings"
    "sync"

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
        keys:            keys,
        materialByKeyId: map[string]*keyMaterial{},
    }
}

type aes256Cipher struct {
    keys KeyProvider

    materialMutex   sync.RWMutex
    materialByKeyId map[string]*keyMaterial
}

/* keyMaterial memoizes, per key id, the AEAD and the sub-key the deterministic nonce is taken under. The key bytes are compared on every read, because a KeyProvider may answer different bytes under the same id. Entries are never evicted, so a provider that rotates to new key ids keeps every retired key resident for the life of the cipher. */
type keyMaterial struct {
    key      []byte
    gcm      cipher.AEAD
    nonceKey []byte
}

func (instance *aes256Cipher) material(keyId string) (*keyMaterial, error) {
    key, keyErr := instance.keys.Key(keyId)
    if nil != keyErr {
        return nil, keyErr
    }

    instance.materialMutex.RLock()
    cached, isCached := instance.materialByKeyId[keyId]
    instance.materialMutex.RUnlock()

    if true == isCached && true == bytes.Equal(cached.key, key) {
        return cached, nil
    }

    gcm, gcmErr := gcmForKey(key, keyId)
    if nil != gcmErr {
        return nil, gcmErr
    }

    built := &keyMaterial{
        key:      bytes.Clone(key),
        gcm:      gcm,
        nonceKey: nonceSubKey(key),
    }

    instance.materialMutex.Lock()
    instance.materialByKeyId[keyId] = built
    instance.materialMutex.Unlock()

    return built, nil
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

/* EncryptDeterministicWithKeyId seals the plaintext deterministically under the named key. A value that is already one of this cipher's seals is converted in place under the key id it carries, never under the named key, so the call is not a rotation. */
func (instance *aes256Cipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    return instance.sealDeterministic(plaintext, keyId)
}

/* sealDeterministic asks one question more than isPassThroughCiphertext: a random-nonce seal authenticates but is a value CiphertextCandidates never produces, so it is converted in place under its own key id, while a deterministic seal passes through whatever key it carries. */
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

/* authenticatedSeal is the write side's classification: every failure answers "not a seal", so a marker-shaped string is read as application data (see isPassThroughCiphertext). */
func (instance *aes256Cipher) authenticatedSeal(value string) (openedSeal, bool) {
    if false == hasEncryptionMarker(value) {
        return openedSeal{}, false
    }

    opened, openErr := instance.openSeal(value)
    if nil != openErr {
        return openedSeal{}, false
    }

    return openedSeal{
        plaintext: opened.plaintext,
        keyId:     opened.keyId,
        deterministic: hmac.Equal(
            opened.nonce,
            deterministicNonceFrom(opened.material.nonceKey, opened.plaintext, opened.material.gcm.NonceSize()),
        ),
    }, true
}

type openedBody struct {
    plaintext string
    keyId     string
    material  *keyMaterial
    nonce     []byte
}

/* openSeal is the opener behind both sides. The caller establishes the marker first, since the write side reads its absence as "not a seal" and the read side as plaintext to pass through. */
func (instance *aes256Cipher) openSeal(encoded string) (openedBody, error) {
    keyId, payload, decodeErr := decodeEncrypted(encoded)
    if nil != decodeErr {
        return openedBody{}, decodeErr
    }

    material, materialErr := instance.material(keyId)
    if nil != materialErr {
        return openedBody{}, materialErr
    }

    nonce := payload[:material.gcm.NonceSize()]

    plaintext, openErr := material.gcm.Open(nil, nonce, payload[material.gcm.NonceSize():], nil)
    if nil != openErr {
        return openedBody{}, exception.NewError("could not decrypt value", map[string]any{"keyId": keyId}, openErr)
    }

    return openedBody{
        plaintext: string(plaintext),
        keyId:     keyId,
        material:  material,
        nonce:     nonce,
    }, nil
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

/* Decrypt returns the plaintext of a value this cipher sealed and passes a value without the marker straight through, so a column can be converted one write at a time. A value that carries the marker but whose body does not parse or authenticate is an error, never plaintext, since only seal writes the marker. */
func (instance *aes256Cipher) Decrypt(encoded string) (string, error) {
    if false == hasEncryptionMarker(encoded) {
        return encoded, nil
    }

    opened, openErr := instance.openSeal(encoded)
    if nil != openErr {
        return "", openErr
    }

    return opened.plaintext, nil
}

/* the write side is deliberately lenient: a marker-shaped value passes through only when it authenticates under a key still in the set, so a retired key's seal is not encrypted twice, and anything else is application data and is sealed under the current key */
func (instance *aes256Cipher) isPassThroughCiphertext(value string) bool {
    if false == hasEncryptionMarker(value) {
        return false
    }

    _, decryptErr := instance.Decrypt(value)

    return nil == decryptErr
}

func (instance *aes256Cipher) seal(plaintext string, keyId string, deterministic bool) (string, error) {
    /* the key id is part of the wire format, in front of a ":" separator, and a custom KeyProvider's id reaches this door unchecked: an empty id or one carrying a colon would write a value that can never be split back apart */
    if false == keyIdPattern.MatchString(keyId) {
        return "", exception.NewError("encryption key id must match "+keyIdPattern.String(), map[string]any{"keyId": keyId}, nil)
    }

    material, materialErr := instance.material(keyId)
    if nil != materialErr {
        return "", materialErr
    }

    var nonce []byte
    if true == deterministic {
        nonce = deterministicNonceFrom(material.nonceKey, plaintext, material.gcm.NonceSize())
    } else {
        nonce = make([]byte, material.gcm.NonceSize())
        if _, readErr := rand.Read(nonce); nil != readErr {
            return "", exception.NewError("could not generate a nonce", nil, readErr)
        }
    }

    ciphertext := material.gcm.Seal(nonce, nonce, []byte(plaintext), nil)

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

/* keyIdOf reports the key id a stored value was sealed under. A value without the marker reports none, and a marker whose payload does not parse is an error, so a migration stops on it instead of sealing it again. */
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

/* nonceSubKey derives the sub-key the deterministic nonce is taken under; it reads the key alone, so it is memoized in keyMaterial. */
func nonceSubKey(key []byte) []byte {
    subKeyMac := hmac.New(sha256.New, key)
    subKeyMac.Write([]byte(deterministicNonceLabel))

    return subKeyMac.Sum(nil)
}

func deterministicNonceFrom(nonceKey []byte, plaintext string, size int) []byte {
    nonceMac := hmac.New(sha256.New, nonceKey)
    nonceMac.Write([]byte(plaintext))

    return nonceMac.Sum(nil)[:size]
}

/* hasEncryptionMarker reports whether a value carries the marker, which only seal writes: its presence is a claim of provenance, and the body behind it must then be a well-formed seal. */
func hasEncryptionMarker(value string) bool {
    return strings.HasPrefix(value, markerPrefix)
}

/* decodeEncrypted splits the body behind an established marker into its key id and payload, and refuses a body that is not one. The payload floor is the nonce plus the authentication tag, the length of a seal of the empty string, so a shortfall is damage rather than an authentication failure. */
func decodeEncrypted(value string) (string, []byte, error) {
    body := value[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator || 0 == separator {
        return "", nil, exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := body[:separator]

    /* Strict refuses a non-canonical final base64 quantum: CiphertextCandidates emits only the canonical spelling, so a lenient decode would authenticate a value the equality lookup can never find */
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
