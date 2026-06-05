package encrypt

import (
    "regexp"
    "sort"

    "github.com/precision-soft/melody/v3/exception"
)

var keyIdPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)

type KeyProvider interface {
    CurrentKeyId() string

    /** ActiveKeyIds returns every usable key id, the current one first, the rest in stable order. */
    ActiveKeyIds() []string

    Key(keyId string) ([]byte, error)
}

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

        copied[keyId] = append([]byte{}, key...)
    }

    return &StaticKeyProvider{
        currentKeyId: currentKeyId,
        keysById:     copied,
    }
}

type StaticKeyProvider struct {
    currentKeyId string
    keysById     map[string][]byte
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
