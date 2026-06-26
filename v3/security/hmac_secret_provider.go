package security

import (
    "github.com/precision-soft/melody/v3/exception"
)

/* HmacSecretProvider resolves the shared secret for the internal-auth HMAC source. CurrentKeyId names the key a signer should use now; Secret looks up the secret for a key id presented on an incoming envelope. Keeping several keys resolvable at once is what makes rotation seamless: roll a new current key while the previous key stays resolvable until every caller has moved, then drop it. This mirrors the encrypt KeyProvider shape. */
type HmacSecretProvider interface {
    CurrentKeyId() string

    Secret(keyId string) ([]byte, bool)
}

func NewStaticHmacSecretProvider(currentKeyId string, secretsByKeyId map[string][]byte) *StaticHmacSecretProvider {
    if "" == currentKeyId {
        exception.Panic(exception.NewError("hmac current key id is empty", nil, nil))
    }

    if 0 == len(secretsByKeyId) {
        exception.Panic(exception.NewError("hmac secrets are empty", nil, nil))
    }

    copied := make(map[string][]byte, len(secretsByKeyId))
    for keyId, secret := range secretsByKeyId {
        if "" == keyId {
            exception.Panic(exception.NewError("hmac key id is empty", nil, nil))
        }

        if 0 == len(secret) {
            exception.Panic(
                exception.NewError("hmac secret is empty", map[string]any{"keyId": keyId}, nil),
            )
        }

        copied[keyId] = append([]byte{}, secret...)
    }

    if _, exists := copied[currentKeyId]; false == exists {
        exception.Panic(
            exception.NewError(
                "hmac current key id has no secret",
                map[string]any{"keyId": currentKeyId},
                nil,
            ),
        )
    }

    return &StaticHmacSecretProvider{
        currentKeyId:   currentKeyId,
        secretsByKeyId: copied,
    }
}

type StaticHmacSecretProvider struct {
    currentKeyId   string
    secretsByKeyId map[string][]byte
}

func (instance *StaticHmacSecretProvider) CurrentKeyId() string {
    return instance.currentKeyId
}

func (instance *StaticHmacSecretProvider) Secret(keyId string) ([]byte, bool) {
    secret, exists := instance.secretsByKeyId[keyId]
    if false == exists {
        return nil, false
    }

    return append([]byte{}, secret...), true
}

var _ HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
