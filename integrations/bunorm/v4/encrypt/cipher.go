package encrypt

import (
    "bytes"
    "crypto/aes"
    "crypto/cipher"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "regexp"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v4/exception"
)

const (
    encryptionMarker = "<ENC>"
    markerGlue       = "\x00"
    formatGcmV1      = "gcm1"
    formatGcmV2      = "gcm2"

    deterministicNonceLabel = "melody/encrypt/deterministic-nonce/v1"

    boundDeterministicNonceLabel = "melody/encrypt/deterministic-nonce/v2"

    sealBindingDomain = "melody/encrypt/seal-binding"

    minNonceSize = 12

    gcmTagOverhead = 16
)

var markerPrefix = encryptionMarker + markerGlue + formatGcmV1 + markerGlue

var boundMarkerPrefix = encryptionMarker + markerGlue + formatGcmV2 + markerGlue

var markerFamily = encryptionMarker + markerGlue

var reportableSealFormatPattern = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

type Cipher interface {
    Encrypt(plaintext string) (string, error)

    EncryptWithKeyId(plaintext string, keyId string) (string, error)

    EncryptDeterministic(plaintext string) (string, error)

    EncryptDeterministicWithKeyId(plaintext string, keyId string) (string, error)

    /* CiphertextCandidates answers the unbound gcm1 candidates, one per active key; a bound column writes gcm2, which none of them matches, so its lookup goes through CiphertextCandidatesFor */
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

/* keyMaterial memoizes, per key id, the AEAD and the two sub-keys the deterministic nonces of an unbound and of a bound seal are taken under. The key bytes are compared on every read, because a KeyProvider may answer different bytes under the same id. Entries are never evicted, so a provider that rotates to new key ids keeps every retired key resident for the life of the cipher. */
type keyMaterial struct {
    key           []byte
    gcm           cipher.AEAD
    nonceKey      []byte
    boundNonceKey []byte
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
        key:           bytes.Clone(key),
        gcm:           gcm,
        nonceKey:      nonceSubKey(key, deterministicNonceLabel),
        boundNonceKey: nonceSubKey(key, boundDeterministicNonceLabel),
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
    format        string
}

/* authenticatedSeal is the write side's classification: every failure answers "not a seal", so a marker-shaped string is read as application data (see isPassThroughCiphertext). */
func (instance *aes256Cipher) authenticatedSeal(value string) (openedSeal, bool) {
    if false == hasEncryptionMarker(value) {
        return openedSeal{}, false
    }

    opened, openErr := instance.openUnboundSeal(value)
    if nil != openErr {
        return openedSeal{}, false
    }

    return opened, true
}

/* openUnboundSeal opens a gcm1 seal and reads its mode back, the one classification behind the write side's authenticatedSeal and the bound reader's admission of an unbound seal */
func (instance *aes256Cipher) openUnboundSeal(value string) (openedSeal, error) {
    opened, openErr := instance.openSeal(value)
    if nil != openErr {
        return openedSeal{}, openErr
    }

    return openedSeal{
        plaintext: opened.plaintext,
        keyId:     opened.keyId,
        deterministic: hmac.Equal(
            opened.nonce,
            deterministicNonceFrom(opened.material.nonceKey, opened.plaintext, opened.material.gcm.NonceSize()),
        ),
        format: formatGcmV1,
    }, nil
}

type openedBody struct {
    plaintext      string
    keyId          string
    material       *keyMaterial
    nonce          []byte
    associatedData []byte
}

/* openSeal is the opener behind both sides for an unbound seal. The caller establishes the marker first, since the write side reads its absence as "not a seal" and the read side as plaintext to pass through. */
func (instance *aes256Cipher) openSeal(encoded string) (openedBody, error) {
    return instance.openSealUnder(encoded, nil)
}

/* openSealUnder opens a gcm1 seal with no associated data when binding is nil, and a gcm2 seal under the binding's associated data otherwise; GCM cannot tell a seal relocated from another binding from a damaged one, so both answer the same error naming the key id and the binding */
func (instance *aes256Cipher) openSealUnder(encoded string, binding *sealBinding) (openedBody, error) {
    prefix := markerPrefix
    if nil != binding {
        prefix = boundMarkerPrefix
    }

    keyId, payload, decodeErr := decodeEncrypted(encoded, prefix)
    if nil != decodeErr {
        return openedBody{}, decodeErr
    }

    material, materialErr := instance.material(keyId)
    if nil != materialErr {
        return openedBody{}, materialErr
    }

    var associatedData []byte
    openContext := map[string]any{"keyId": keyId}
    if nil != binding {
        associatedData = sealAssociatedData(formatGcmV2, keyId, binding.label)
        openContext["sealBinding"] = binding.label
    }

    nonce := payload[:material.gcm.NonceSize()]

    plaintext, openErr := material.gcm.Open(nil, nonce, payload[material.gcm.NonceSize():], associatedData)
    if nil != openErr {
        return openedBody{}, exception.NewError("could not decrypt value", openContext, openErr)
    }

    return openedBody{
        plaintext:      string(plaintext),
        keyId:          keyId,
        material:       material,
        nonce:          nonce,
        associatedData: associatedData,
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

/* Decrypt returns the plaintext of a value this cipher sealed and passes a value without the marker straight through, so a column can be converted one write at a time. A value that carries the marker but whose body does not parse or authenticate is an error, never plaintext, since only seal writes the marker, and so is a value of the marker family under a format this build does not read, ErrSealFormatUnknown in its chain, and a seal bound to a column, ErrSealBound in its chain, since it opens under its column's binding alone. */
func (instance *aes256Cipher) Decrypt(encoded string) (string, error) {
    switch sealShapeOf(encoded) {
    case sealShapeNone:
        return encoded, nil
    case sealShapeGcmV2:
        return "", errSealBound()
    case sealShapeUnknown:
        return "", errSealFormatUnknown(encoded)
    }

    opened, openErr := instance.openSeal(encoded)
    if nil != openErr {
        return "", openErr
    }

    return opened.plaintext, nil
}

/* valueSealer is the door the column types and the Migrator seal through. A value is always sealed as data, even when it is itself one of this cipher's seals: a column an attacker can write and later read back in the clear would otherwise hand back the plaintext of any seal of the compartment, from any row, column or table. The Migrator classifies the STORED value through authenticatedSeal and seals the value it decrypted through sealValueWithKeyId, so nothing it re-writes is inspected for being a seal. A Cipher implemented outside this package does not carry the door, so a column type over it refuses a value carrying the encryption marker instead of risking a bare seal: a wrapper's Encrypt may hand such a value back as it came. */
type valueSealer interface {
    sealValue(plaintext string, deterministic bool) (string, error)

    sealValueWithKeyId(plaintext string, keyId string, deterministic bool) (string, error)

    authenticatedSeal(value string) (openedSeal, bool)
}

func (instance *aes256Cipher) sealValue(plaintext string, deterministic bool) (string, error) {
    return instance.seal(plaintext, instance.keys.CurrentKeyId(), deterministic)
}

func (instance *aes256Cipher) sealValueWithKeyId(plaintext string, keyId string, deterministic bool) (string, error) {
    return instance.seal(plaintext, keyId, deterministic)
}

/* sealBinding is a declared binding as a column reads it: the label its seals are bound to, and whether the column refuses a value that is not bound to it */
type sealBinding struct {
    label         string
    refuseUnbound bool
}

/* bindingSealer is the door a bound column type seals, opens and looks up through, carried by the package cipher and the fake; a Cipher implemented outside the package does not carry it, and a bound column over one is refused, since sealing unbound would drop the binding without a word */
type bindingSealer interface {
    sealBoundValue(plaintext string, binding sealBinding, deterministic bool) (string, error)

    /* sealBoundValueWithKeyId is the Migrator's door: a value re-sealed under the binding keeps the key it was read under, or takes the rotation's target */
    sealBoundValueWithKeyId(plaintext string, keyId string, binding sealBinding, deterministic bool) (string, error)

    /* openStored reads a stored value as the column of the binding does: a gcm2 seal under its label, and a gcm1 seal or a value without the marker only while the binding admits them, refused before any key is asked for otherwise */
    openStored(stored string, binding sealBinding) (openedSeal, error)

    boundCandidates(plaintext string, binding sealBinding) ([][]byte, error)
}

func (instance *aes256Cipher) sealBoundValue(plaintext string, binding sealBinding, deterministic bool) (string, error) {
    return instance.sealUnder(plaintext, instance.keys.CurrentKeyId(), deterministic, &binding)
}

func (instance *aes256Cipher) sealBoundValueWithKeyId(plaintext string, keyId string, binding sealBinding, deterministic bool) (string, error) {
    return instance.sealUnder(plaintext, keyId, deterministic, &binding)
}

func (instance *aes256Cipher) openStored(stored string, binding sealBinding) (openedSeal, error) {
    shape := sealShapeOf(stored)

    if sealShapeUnknown == shape {
        return openedSeal{}, errSealFormatUnknown(stored)
    }

    if sealShapeGcmV2 != shape && true == binding.refuseUnbound {
        return openedSeal{}, errUnboundValueRefused(binding)
    }

    if sealShapeNone == shape {
        return openedSeal{plaintext: stored}, nil
    }

    if sealShapeGcmV1 == shape {
        return instance.openUnboundSeal(stored)
    }

    opened, openErr := instance.openSealUnder(stored, &binding)
    if nil != openErr {
        return openedSeal{}, openErr
    }

    return openedSeal{
        plaintext:     opened.plaintext,
        keyId:         opened.keyId,
        deterministic: hmac.Equal(opened.nonce, boundDeterministicNonceFrom(opened.material.boundNonceKey, opened.associatedData, opened.plaintext, opened.material.gcm.NonceSize())),
        format:        formatGcmV2,
    }, nil
}

func (instance *aes256Cipher) boundCandidates(plaintext string, binding sealBinding) ([][]byte, error) {
    keyIds := instance.keys.ActiveKeyIds()

    candidates := make([][]byte, 0, len(keyIds))
    for _, keyId := range keyIds {
        candidate, sealErr := instance.sealUnder(plaintext, keyId, true, &binding)
        if nil != sealErr {
            return nil, sealErr
        }

        candidates = append(candidates, []byte(candidate))
    }

    return candidates, nil
}

/* sealColumnValue seals the value a column type writes, through valueSealer when the cipher carries it */
func sealColumnValue(cipherInstance Cipher, plaintext string, deterministic bool) (string, error) {
    sealer, isSealer := cipherInstance.(valueSealer)
    if true == isSealer {
        return sealer.sealValue(plaintext, deterministic)
    }

    if true == carriesMarkerFamily(plaintext) {
        return "", errSealWrittenAsDataThroughForeignCipher()
    }

    if true == deterministic {
        return cipherInstance.EncryptDeterministic(plaintext)
    }

    return cipherInstance.Encrypt(plaintext)
}

/* errSealWrittenAsDataThroughForeignCipher refuses a marker-shaped value a column type would write through a cipher implemented outside this package; the context carries no key id, since the value is application data and the id it spells is untrusted */
func errSealWrittenAsDataThroughForeignCipher() error {
    return exception.NewError(
        "a column type over a cipher implemented outside this package refuses a value that carries the encryption marker; install the package cipher directly where applications may store such values",
        nil,
        nil,
    )
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
    return instance.sealUnder(plaintext, keyId, deterministic, nil)
}

/* sealUnder writes a gcm1 seal with no associated data when binding is nil, and a gcm2 seal under the binding's associated data otherwise, its deterministic nonce taken over that associated data too: GCM with one nonce over two associated data writes the same body, so a nonce of the plaintext alone would still show equal values across bound columns and reuse the nonce */
func (instance *aes256Cipher) sealUnder(plaintext string, keyId string, deterministic bool, binding *sealBinding) (string, error) {
    /* the key id is part of the wire format, in front of a ":" separator, and a custom KeyProvider's id reaches this door unchecked: an empty id or one carrying a colon would write a value that can never be split back apart */
    if false == keyIdPattern.MatchString(keyId) {
        return "", exception.NewError("encryption key id must match "+keyIdPattern.String(), map[string]any{"keyId": keyId}, nil)
    }

    material, materialErr := instance.material(keyId)
    if nil != materialErr {
        return "", materialErr
    }

    prefix := markerPrefix
    var associatedData []byte
    if nil != binding {
        prefix = boundMarkerPrefix
        associatedData = sealAssociatedData(formatGcmV2, keyId, binding.label)
    }

    var nonce []byte
    if true == deterministic && nil != binding {
        nonce = boundDeterministicNonceFrom(material.boundNonceKey, associatedData, plaintext, material.gcm.NonceSize())
    } else if true == deterministic {
        nonce = deterministicNonceFrom(material.nonceKey, plaintext, material.gcm.NonceSize())
    } else {
        nonce = make([]byte, material.gcm.NonceSize())
        if _, readErr := rand.Read(nonce); nil != readErr {
            return "", exception.NewError("could not generate a nonce", nil, readErr)
        }
    }

    ciphertext := material.gcm.Seal(nonce, nonce, []byte(plaintext), associatedData)

    return prefix + keyId + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
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

/* keyIdOf reports the key id a stored value was sealed under. A value without the marker reports none, and a marker whose payload does not parse, or a marker family under a format this build does not read, is an error, so a migration stops on it instead of sealing it again. */
func keyIdOf(encoded string) (string, bool, error) {
    prefix := markerPrefix
    switch sealShapeOf(encoded) {
    case sealShapeNone:
        return "", false, nil
    case sealShapeUnknown:
        return "", false, errSealFormatUnknown(encoded)
    case sealShapeGcmV2:
        prefix = boundMarkerPrefix
    }

    keyId, _, decodeErr := decodeEncrypted(encoded, prefix)
    if nil != decodeErr {
        return "", false, decodeErr
    }

    return keyId, true, nil
}

/* nonceSubKey derives a sub-key a deterministic nonce is taken under, one per label so an unbound and a bound nonce are never the same function of the same input; it reads the key alone, so it is memoized in keyMaterial. */
func nonceSubKey(key []byte, label string) []byte {
    subKeyMac := hmac.New(sha256.New, key)
    subKeyMac.Write([]byte(label))

    return subKeyMac.Sum(nil)
}

func deterministicNonceFrom(nonceKey []byte, plaintext string, size int) []byte {
    nonceMac := hmac.New(sha256.New, nonceKey)
    nonceMac.Write([]byte(plaintext))

    return nonceMac.Sum(nil)[:size]
}

/* boundDeterministicNonceFrom takes the nonce over the associated data and the plaintext, the NUL after the associated data ending it, since the label it closes on carries none */
func boundDeterministicNonceFrom(boundNonceKey []byte, associatedData []byte, plaintext string, size int) []byte {
    nonceMac := hmac.New(sha256.New, boundNonceKey)
    nonceMac.Write(associatedData)
    nonceMac.Write([]byte(markerGlue))
    nonceMac.Write([]byte(plaintext))

    return nonceMac.Sum(nil)[:size]
}

/* sealAssociatedData is the one spelling of a bound seal's associated data, read by the sealer, the opener and the nonce: the domain, the format, the key id and the label, NUL-separated; none of the four carries a NUL, so the split is unique */
func sealAssociatedData(format string, keyId string, label string) []byte {
    return []byte(sealBindingDomain + markerGlue + format + markerGlue + keyId + markerGlue + label)
}

type sealShape int

const (
    sealShapeNone sealShape = iota
    sealShapeGcmV1
    sealShapeGcmV2
    sealShapeUnknown
)

/* sealShapeOf classifies a stored value once: no marker, an unbound gcm1 seal, a bound gcm2 seal, or the marker family under a format this build does not read */
func sealShapeOf(value string) sealShape {
    switch {
    case true == strings.HasPrefix(value, markerPrefix):
        return sealShapeGcmV1
    case true == strings.HasPrefix(value, boundMarkerPrefix):
        return sealShapeGcmV2
    case true == strings.HasPrefix(value, markerFamily):
        return sealShapeUnknown
    default:
        return sealShapeNone
    }
}

/* hasEncryptionMarker reports whether a value carries the marker, which only seal writes: its presence is a claim of provenance, and the body behind it must then be a well-formed seal. */
func hasEncryptionMarker(value string) bool {
    return strings.HasPrefix(value, markerPrefix)
}

/* carriesMarkerFamily reports whether a value begins with the marker and its glue, whatever format follows: no text carries that NUL by accident, so such a value is a seal of some format, never plaintext. */
func carriesMarkerFamily(value string) bool {
    return strings.HasPrefix(value, markerFamily)
}

func errSealBound() error {
    return exception.NewError(ErrSealBound.Error(), nil, ErrSealBound)
}

func errUnboundValueRefused(binding sealBinding) error {
    return exception.NewError(ErrUnboundValueRefused.Error(), map[string]any{"sealBinding": binding.label}, ErrUnboundValueRefused)
}

/* errSealFormatUnknown names the format only when it is a token a build could have written, and never the key id or the body behind it */
func errSealFormatUnknown(value string) error {
    var context map[string]any

    remainder := value[len(markerFamily):]
    if glue := strings.Index(remainder, markerGlue); -1 != glue && true == reportableSealFormatPattern.MatchString(remainder[:glue]) {
        context = map[string]any{"format": remainder[:glue]}
    }

    return exception.NewError(ErrSealFormatUnknown.Error(), context, ErrSealFormatUnknown)
}

/* decodeEncrypted splits the body behind an established marker into its key id and payload, and refuses a body that is not one. The payload floor is the nonce plus the authentication tag, the length of a seal of the empty string, so a shortfall is damage rather than an authentication failure. */
func decodeEncrypted(value string, prefix string) (string, []byte, error) {
    body := value[len(prefix):]

    separator := strings.IndexByte(body, ':')
    if -1 == separator || 0 == separator {
        return "", nil, exception.NewError("encrypted value is malformed", nil, nil)
    }

    keyId := body[:separator]

    /* the read side holds the key id to the grammar the write side seals under, so an id no seal can carry is malformed and never reaches the KeyProvider or an error context */
    if false == keyIdPattern.MatchString(keyId) {
        return "", nil, exception.NewError("encrypted value is malformed", nil, nil)
    }

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

var _ bindingSealer = (*aes256Cipher)(nil)
