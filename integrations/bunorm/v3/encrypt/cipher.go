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
        keys:            keys,
        materialByKeyId: map[string]*keyMaterial{},
    }
}

type aes256Cipher struct {
    keys KeyProvider

    materialMutex   sync.RWMutex
    materialByKeyId map[string]*keyMaterial
}

/* keyMaterial is what a key yields once instead of once per call: the AEAD built over it, and the sub-key the deterministic nonce is taken under, which depends on the key alone. Measured on the development container, building the AEAD costs about six hundred nanoseconds and 1280 bytes — forty per cent of the time and eighty-seven per cent of the allocation of a single Decrypt — while the deterministic conversion path paid it twice and CiphertextCandidates pays it once per active key on every equality lookup.

   The key bytes are kept beside it and compared on every read, because KeyProvider is a PUBLIC interface: an application's provider is free to answer different bytes under the same id, and a memo that trusted the id alone would seal and open under the retired key long after the provider had rotated it. Comparing a few dozen bytes against six hundred nanoseconds is what makes the memo safe rather than merely fast.

   What the memo RETAINS is written here because nothing else says it: an entry per key id ever seen, each holding the raw key bytes, for the life of the cipher. Nothing evicts one, so a provider that rotates to a NEW key id keeps every retired key resident — measured, two hundred rotations under two hundred ids leave two hundred entries and the first key's bytes still in memory after the provider has dropped it; a rotation that keeps the id replaces the entry, and two hundred of those leave one. For the shipped StaticKeyProvider, whose keys live as long as the process anyway, that is no change; for a rotating provider it is a property to know about, and bounding it is a decision about the component rather than about this memo. */
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

/* EncryptDeterministicWithKeyId seals the plaintext deterministically under the named key. The key id is honoured for a plaintext this cipher has not sealed; a value that is already one of its seals is converted in place under the key id THAT value carries — a random-nonce seal becomes deterministic under its own key, a deterministic one passes through — never under the named key, so the conversion is not a rotation. The rule and its reason are on sealDeterministic. */
func (instance *aes256Cipher) EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error) {
    return instance.sealDeterministic(plaintext, keyId)
}

/* sealDeterministic is the deterministic doors' pass-through, which asks one question more than the random doors' isPassThroughCiphertext: not only "does this value authenticate under a key in the set" but "is its nonce the deterministic one for its own plaintext under its own key". A random-nonce seal handed to a deterministic column authenticates all the same, and passing it through stored a value CiphertextCandidates can never produce, so the equality lookup answered no rows for a row whose plaintext it held. Such a seal is converted in place — its plaintext sealed deterministically under the key id it already carries, never under the door's key, so the conversion is not a key rotation — which is the rule the bulk migration already applies to a stored column; a seal that is deterministic already passes through whatever key it carries, so a retired key's ciphertexts survive until re-encryption, exactly as on the random doors. */
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

/* openedSeal is what a value this cipher wrote yields once opened: the plaintext, the key id it carries and whether the nonce it was sealed with is the deterministic one for that plaintext under that key. */
type openedSeal struct {
    plaintext     string
    keyId         string
    deterministic bool
}

/* authenticatedSeal classifies a value for the write side: it answers whether the value is a seal this cipher can open under a key still in the set, and what that seal holds. Every failure — no marker, a body that does not parse, an unknown key id, a payload that does not authenticate — answers "not a seal", which is the write side's lenient reading of a marker-shaped string as application data (see isPassThroughCiphertext). */
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

/* openedBody is what the shared opener yields: the plaintext, the key id the value was sealed under, and the material and nonce a caller needs to ask anything FURTHER about it — which only the write side does, to tell a deterministic nonce from a random one. The read side takes the plaintext and pays for nothing else. */
type openedBody struct {
    plaintext string
    keyId     string
    material  *keyMaterial
    nonce     []byte
}

/* openSeal is the one opener behind both sides. The caller establishes the marker first, because that is where the two genuinely differ — the write side reads its absence as "not a seal", the read side as a value to pass straight through — and everything after it is the same four steps: split the body, resolve the key material, take the nonce off the front of the payload, open the rest under it.

   The failure of the open itself is named here in the read side's wording, because it is the only one of the four that is about the VALUE rather than about the key set; the write side maps every failure onto "not a seal" regardless, so naming this one costs it nothing.

   No length re-check on the nonce: decodeEncrypted floors the payload at nonce + tag, and gcmForKey pins the nonce at exactly minNonceSize, so a second floor was a dead branch no test could ever reach. */
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

/* Decrypt returns the plaintext of a value this cipher sealed, and passes a value that carries no marker straight through: a column is converted one write at a time, so the rows not yet sealed must keep reading, and that pass-through is what makes an incremental migration possible.

   A value that DOES carry the marker is not eligible for that pass-through. The marker is written by seal and by nothing else, so its presence is the framework's own claim that the value was encrypted here; a body behind it that no longer parses is damage. The way it happens in practice is a column too narrow for the ciphertext under a non-strict sql_mode, where MySQL truncates the value the UPDATE wrote and reports a warning instead of failing. Handing the caller that fragment as though the application had stored it is silent corruption — the row reads as a marker, a key id and half a base64 blob, and every later write and comparison builds on it. It is an error instead. */
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

/* a marker-shaped plaintext must not be stored as-is: it would poison every later Scan/Decrypt. Pass through only values that authenticate under a key currently in the key set. A retired key stays in the set (still decryptable) until re-encryption completes and is only then removed, so a value sealed under it is not destroyed by double encryption; a marker-shaped value bearing an unknown key id, or one whose payload does not parse at all, is treated as ordinary plaintext and sealed under the current key instead of being stored verbatim.

   This is the write side, and it is deliberately the lenient one: what arrives here is application data, and an application is free to hold a string that merely looks like a marker. Sealing it is the safe answer. Reading is the strict side — a marker that comes back OUT of the database was put there by this cipher, so a payload that no longer parses is reported rather than passed off as plaintext.

   This is the random doors' question, and it is the whole of it: a seal that authenticates is confidential whichever nonce it was sealed with. The deterministic doors ask one question more, in sealDeterministic, because for them a random-nonce seal is a value the equality lookup can never find. */
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

/* nonceSubKey derives the sub-key the deterministic nonce is taken under. It reads the key and nothing else, so it belongs beside the AEAD in keyMaterial rather than inside every seal: measured, the two halves of the old one-shot cost roughly seven hundred nanoseconds each, and only the second depends on the plaintext. */
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
