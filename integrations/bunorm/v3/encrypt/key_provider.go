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

/* NewStaticKeyProvider holds the keys it is handed for the life of the process. Every key must be 32 bytes, the AES-256 size, and must not be all zero bytes: that is the key a buffer nobody wrote to has, the shape of a key file that was never generated or a variable that was never set, and it sealed every column under a key any reader can guess. Nothing beyond that is judged — a key is the operator's, generated from crypto/rand, and this door cannot tell a strong one from a weak one. */
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

/* GoString and String keep the master keys out of every rendering fmt can reach: %#v and %v walk unexported fields, so a provider dropped into a debug log — or into an error context that is formatted later — printed each key as raw bytes. The receivers are values so that both the provider and a pointer to it redact, and the current key id is kept because it names a key without revealing one. */
func (instance StaticKeyProvider) GoString() string {
    return instance.String()
}

func (instance StaticKeyProvider) String() string {
    return "encrypt.StaticKeyProvider{currentKeyId:" + instance.currentKeyId + ", keysById:[redacted]}"
}

/* Format keeps the master keys redacted for the numeric verbs (%d %o %b %c %U) that fmt never routes through Stringer or GoStringer: fmt consults those interfaces only for %v %s %q %x %X and %#v, so a numeric verb would otherwise reflection-walk the unexported keysById field and dump the raw key bytes. Every verb is answered with the same redacted String() rendering, and the value receiver makes both the provider and a pointer to it satisfy fmt.Formatter. */
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
