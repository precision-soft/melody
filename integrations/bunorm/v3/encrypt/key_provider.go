package encrypt

import (
    "fmt"
    "regexp"
    "sort"

    "github.com/precision-soft/melody/v3/exception"
)

var keyIdPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)

/* KeyProvider hands the cipher its keys. The contract has three obligations the compiler cannot see. Every id CurrentKeyId or ActiveKeyIds answers must stay resolvable through Key for as long as it is answered — the cipher reads in two steps (the id, then the key), so a provider that retires an id between the two fails a write that should have succeeded. Every id must match the key id grammar (`^[A-Za-z0-9_.-]{1,32}$`): the id is written into the stored value in front of a ":" separator, and seal refuses one that would corrupt the wire format. And a retired key must STAY in the set until every value sealed under it has been re-encrypted — the cipher treats a marker-shaped value under an unknown id as ordinary plaintext and seals it, so dropping a key while its ciphertexts remain makes them unrecoverable at the next write that touches them. */
type KeyProvider interface {
    CurrentKeyId() string

    ActiveKeyIds() []string

    Key(keyId string) ([]byte, error)
}

/* NewStaticKeyProvider holds the keys it is handed for the life of the process. Every key must be 32 bytes, the AES-256 size, and not all zero bytes, the key of a buffer nobody wrote to; nothing beyond that is judged. */
func NewStaticKeyProvider(currentKeyId string, keysById map[string][]byte) *StaticKeyProvider {
    if "" == currentKeyId {
        exception.Panic(exception.NewError("current key id is empty", nil, nil))
    }

    if _, exists := keysById[currentKeyId]; false == exists {
        exception.Panic(exception.NewError("current key id has no key", map[string]any{"keyId": currentKeyId}, nil))
    }

    copied := make(map[string][]byte, len(keysById))
    for keyId, key := range keysById {
        if false == keyIdPattern.MatchString(keyId) {
            exception.Panic(exception.NewError("key id must match "+keyIdPattern.String(), map[string]any{"keyId": keyId}, nil))
        }

        if 32 != len(key) {
            exception.Panic(exception.NewError("encryption key must be 32 bytes for aes-256", map[string]any{"keyId": keyId, "length": len(key)}, nil))
        }

        if true == isAllZeroKey(key) {
            exception.Panic(exception.NewError("encryption key is all zero bytes; generate the key from crypto/rand", map[string]any{"keyId": keyId}, nil))
        }

        copied[keyId] = append([]byte{}, key...)
    }

    return &StaticKeyProvider{
        currentKeyId: currentKeyId,
        keysById:     copied,
    }
}

func isAllZeroKey(key []byte) bool {
    for _, keyByte := range key {
        if 0 != keyByte {
            return false
        }
    }

    return true
}

type StaticKeyProvider struct {
    currentKeyId string
    keysById     map[string][]byte
}

/* Deprecated: GoString is never reached — fmt consults Formatter before GoStringer, and this type implements Format, so Format answers %#v too. It is kept only because it was released in integrations/bunorm/v3.2.0 and dropping an exported method is a breaking change; it is removed at v4. String and Format carry the redaction. */
func (instance StaticKeyProvider) GoString() string {
    return instance.String()
}

/* String keeps the master keys out of every rendering fmt routes through this value's methods, naming only the current key id. A provider held in an unexported field of another struct is walked by reflection with no method called, so it is not embedded that way. */
func (instance StaticKeyProvider) String() string {
    return "encrypt.StaticKeyProvider{currentKeyId:" + instance.currentKeyId + ", keysById:[redacted]}"
}

/* Format answers every verb that reaches it, the numeric ones included, with the String rendering, so no verb walks the keys by reflection. %p and %w never reach it: fmt prints the operand by reflection before consulting any method. */
func (instance StaticKeyProvider) Format(state fmt.State, verb rune) {
    _, _ = state.Write([]byte(instance.String()))
}

func (instance *StaticKeyProvider) CurrentKeyId() string {
    return instance.currentKeyId
}

func (instance *StaticKeyProvider) ActiveKeyIds() []string {
    others := make([]string, 0, len(instance.keysById))
    for keyId := range instance.keysById {
        if keyId != instance.currentKeyId {
            others = append(others, keyId)
        }
    }

    sort.Strings(others)

    return append([]string{instance.currentKeyId}, others...)
}

func (instance *StaticKeyProvider) Key(keyId string) ([]byte, error) {
    key, exists := instance.keysById[keyId]
    if false == exists {
        return nil, exception.NewError("encryption key not found", map[string]any{"keyId": keyId}, nil)
    }

    return append([]byte{}, key...), nil
}

var _ KeyProvider = (*StaticKeyProvider)(nil)

var _ fmt.Formatter = StaticKeyProvider{}
