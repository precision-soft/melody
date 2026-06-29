package security

import (
    "github.com/precision-soft/melody/v3/exception"
)

/* HmacSecretProvider resolves the shared secret for the internal-auth HMAC source. CurrentKeyId names the key a signer should use now; Secret looks up the secret for a key id presented on an incoming envelope; AppForKeyId names the single application a key id is issued to, so the verifier can refuse an envelope whose claimed app does not own the key that signed it. Keeping several keys resolvable at once is what makes rotation seamless: roll a new current key while the previous key stays resolvable until every caller has moved, then drop it. This mirrors the encrypt KeyProvider shape. */
type HmacSecretProvider interface {
    CurrentKeyId() string

    Secret(keyId string) ([]byte, bool)

    AppForKeyId(keyId string) (string, bool)
}

/* HmacKey is one entry in a static secret provider: the secret bytes for a key id and the application that key id belongs to. A key id is owned by exactly one app; an app may own several key ids (rotation overlap), all naming the same app. */
type HmacKey struct {
    App    string
    Secret []byte
}

func NewStaticHmacSecretProvider(currentKeyId string, keysByKeyId map[string]HmacKey) *StaticHmacSecretProvider {
    if "" == currentKeyId {
        exception.Panic(exception.NewError("hmac current key id is empty", nil, nil))
    }

    if 0 == len(keysByKeyId) {
        exception.Panic(exception.NewError("hmac secrets are empty", nil, nil))
    }

    secretsByKeyId := make(map[string][]byte, len(keysByKeyId))
    appsByKeyId := make(map[string]string, len(keysByKeyId))
    appBySecret := make(map[string]string, len(keysByKeyId))
    for keyId, key := range keysByKeyId {
        if "" == keyId {
            exception.Panic(exception.NewError("hmac key id is empty", nil, nil))
        }

        if "" == key.App {
            exception.Panic(
                exception.NewError("hmac key has no app", map[string]any{"keyId": keyId}, nil),
            )
        }

        if 0 == len(key.Secret) {
            exception.Panic(
                exception.NewError("hmac secret is empty", map[string]any{"keyId": keyId}, nil),
            )
        }

        /* the key-id↔app binding only isolates apps if their secret material is distinct: the key id is attacker-visible, so a secret shared across two apps would let a holder sign under either app's key id and defeat the binding. Reject cross-app secret reuse at construction rather than silently re-opening that escalation. */
        if owner, reused := appBySecret[string(key.Secret)]; true == reused && owner != key.App {
            exception.Panic(
                exception.NewError(
                    "hmac secret is reused across apps",
                    map[string]any{"keyId": keyId, "app": key.App, "otherApp": owner},
                    nil,
                ),
            )
        }
        appBySecret[string(key.Secret)] = key.App

        secretsByKeyId[keyId] = append([]byte{}, key.Secret...)
        appsByKeyId[keyId] = key.App
    }

    if _, exists := secretsByKeyId[currentKeyId]; false == exists {
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
        secretsByKeyId: secretsByKeyId,
        appsByKeyId:    appsByKeyId,
    }
}

type StaticHmacSecretProvider struct {
    currentKeyId   string
    secretsByKeyId map[string][]byte
    appsByKeyId    map[string]string
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

func (instance *StaticHmacSecretProvider) AppForKeyId(keyId string) (string, bool) {
    app, exists := instance.appsByKeyId[keyId]
    if false == exists {
        return "", false
    }

    return app, true
}

var _ HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
