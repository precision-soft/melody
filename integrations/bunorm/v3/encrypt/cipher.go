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

    /* gcmTagOverhead is what gcm.Seal appends beyond the plaintext: a stored payload is nonce + ciphertext + tag, so anything shorter than nonce + tag cannot be a seal of even the empty string — a shortfall there is structural damage, not an authentication failure to be blamed on a key */
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
    if true == instance.isPassThroughCiphertext(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, instance.keys.CurrentKeyId(), true)
}

func (instance *aes256Cipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    if true == instance.isPassThroughCiphertext(plaintext) {
        return plaintext, nil
    }

    return instance.seal(plaintext, keyId, true)
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

/* Decrypt returns the plaintext of a value this cipher sealed, and passes a value that carries no marker straight through: a column is converted one write at a time, so the rows not yet sealed must keep reading, and that pass-through is what makes an incremental migration possible.

   A value that DOES carry the marker is not eligible for that pass-through. The marker is written by seal and by nothing else, so its presence is the framework's own claim that the value was encrypted here; a body behind it that no longer parses is damage. The way it happens in practice is a column too narrow for the ciphertext under a non-strict sql_mode, where MySQL truncates the value the UPDATE wrote and reports a warning instead of failing. Handing the caller that fragment as though the application had stored it is silent corruption — the row reads as a marker, a key id and half a base64 blob, and every later write and comparison builds on it. It is an error instead. */
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

    /* no length re-check here: decodeEncrypted floors the payload at nonce + tag, and gcmForKey pins the nonce at exactly minNonceSize, so a second floor on the nonce alone was a dead branch no test could ever reach */
    nonce := payload[:gcm.NonceSize()]
    ciphertext := payload[gcm.NonceSize():]

    plaintext, openErr := gcm.Open(nil, nonce, ciphertext, nil)
    if nil != openErr {
        return "", exception.NewError("could not decrypt value", map[string]any{"keyId": keyId}, openErr)
    }

    return string(plaintext), nil
}

/* a marker-shaped plaintext must not be stored as-is: it would poison every later Scan/Decrypt. Pass through only values that authenticate under a key currently in the key set. A retired key stays in the set (still decryptable) until re-encryption completes and is only then removed, so a value sealed under it is not destroyed by double encryption; a marker-shaped value bearing an unknown key id, or one whose payload does not parse at all, is treated as ordinary plaintext and sealed under the current key instead of being stored verbatim.

   This is the write side, and it is deliberately the lenient one: what arrives here is application data, and an application is free to hold a string that merely looks like a marker. Sealing it is the safe answer. Reading is the strict side — a marker that comes back OUT of the database was put there by this cipher, so a payload that no longer parses is reported rather than passed off as plaintext. */
func (instance *aes256Cipher) isPassThroughCiphertext(value string) bool {
    if false == hasEncryptionMarker(value) {
        return false
    }

    _, decryptErr := instance.Decrypt(value)

    return nil == decryptErr
}

func (instance *aes256Cipher) seal(plaintext string, keyId string, deterministic bool) (string, error) {
    /* the key id is written into the stored value in front of a ":" separator, so its grammar is part of the wire format: an empty id, or one carrying a colon, produces a value decodeEncrypted can never split back apart — the write succeeds and the loss surfaces only at the next read, permanently. StaticKeyProvider enforces this grammar at construction, but KeyProvider is a public interface and a custom provider's id reaches this door unchecked. */
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

/* keyIdOf reports the key id a stored value was sealed under. A value carrying no marker is ordinary plaintext and reports no key id, which is how a bulk migration tells a converted row from one still waiting. A value carrying the marker whose payload no longer parses is damage and is reported as an error, so a migration stops on it instead of classifying it as plaintext and sealing the wreckage a second time. */
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

/* hasEncryptionMarker reports whether a value carries the framework's own encryption marker. The marker is emitted by seal and by nothing else, so it is a claim of provenance rather than a heuristic: a value that carries it came out of this cipher, and what follows it is required to be a well-formed sealed body. Provenance and well-formedness are deliberately separate questions — conflating them is what let a truncated ciphertext be mistaken for plaintext. */
func hasEncryptionMarker(value string) bool {
    return strings.HasPrefix(value, markerPrefix)
}

/* decodeEncrypted splits the body behind the marker into its key id and its raw payload, and refuses a body that is no longer one.

   The caller must have established the marker first: this reads the body positionally and says nothing about values that carry no marker, which are ordinary plaintext and belong to no key. Every failure here means the same thing — a value this cipher wrote came back changed — so each is reported rather than absorbed. The length floor is the nonce plus the authentication tag: a seal of even the empty string is never shorter, so a shortfall is structural damage and not an authentication failure to be blamed on a key. */
func decodeEncrypted(value string) (string, []byte, error) {
    body := value[len(markerPrefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator || 0 == separator {
        return "", nil, exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := body[:separator]

    /* Strict rejects a final base64 quantum with non-zero discarded bits: the lenient decoder maps several spellings onto the same bytes, so an altered last character still authenticated while CiphertextCandidates only ever emits the canonical spelling — a deterministic equality lookup missed a row whose plaintext it held. Everything seal writes is canonical, so Strict refuses only what this cipher never produced. */
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
