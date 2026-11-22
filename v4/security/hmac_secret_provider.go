package security

import (
    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* HmacSecretProvider resolves the shared secret for the internal-auth HMAC source. CurrentKeyId names the key a signer uses now, Secret looks up the secret for a presented key id, and ApplicationForKeyId names the one application a key id is issued to. Several resolvable keys allow a rotation without a gap. Every door takes the caller's runtime first: the request's on the verifying side, and on the signing side the boot runtime the signer was built under and the caller's runtime at Sign. A nil runtime is refused. An answer of false says the key id is not known and refuses the envelope; an error says the provider could not answer, and is never used for an unknown key: the verifier answers it as unavailable, the signer returns it marked with exception.MarkInfrastructureFailure. */
type HmacSecretProvider interface {
    CurrentKeyId(runtimeInstance containercontract.Runtime) (string, error)

    Secret(runtimeInstance containercontract.Runtime, keyId string) ([]byte, bool, error)

    ApplicationForKeyId(runtimeInstance containercontract.Runtime, keyId string) (string, bool, error)
}

/* HmacKey is one entry in a static secret provider: the secret bytes for a key id and the application that key id belongs to. A key id is owned by exactly one application; an application may own several key ids (rotation overlap), all naming the same application. */
type HmacKey struct {
    Application string
    Secret      []byte
}

/* HmacKeyRing is the set of keys a static provider resolves: CurrentKeyId names the key a signer uses now, Keys every resolvable key by id, each bound to its application; both are required and judged when the provider is built. */
type HmacKeyRing struct {
    CurrentKeyId string
    Keys         map[string]HmacKey

    /* MinimumSecretLength is the fewest bytes each key's secret may hold: 32 by default, the RFC 7518 floor for an HMAC-SHA256 key, at least 32, unlimited refused; a shorter secret is refused naming HmacKeyRing.Keys and its key id. */
    MinimumSecretLength bound.Count
}

/* NewStaticHmacSecretProvider judges the key ring, panicking on a refused one naming the field, a secret shorter than MinimumSecretLength included, and builds the provider over a copy of it. */
func NewStaticHmacSecretProvider(ring HmacKeyRing) *StaticHmacSecretProvider {
    currentKeyId := ring.CurrentKeyId
    keysByKeyId := ring.Keys
    minimumSecretLength := resolveMinimumSecretLength(ring.MinimumSecretLength, "HmacKeyRing.MinimumSecretLength")

    if "" == currentKeyId {
        exception.Panic(exception.NewError("hmac current key id is empty", map[string]any{"field": "HmacKeyRing.CurrentKeyId"}, nil))
    }

    if 0 == len(keysByKeyId) {
        exception.Panic(exception.NewError("hmac secrets are empty", map[string]any{"field": "HmacKeyRing.Keys"}, nil))
    }

    secretsByKeyId := make(map[string][]byte, len(keysByKeyId))
    applicationsByKeyId := make(map[string]string, len(keysByKeyId))
    applicationBySecret := make(map[string]string, len(keysByKeyId))
    for keyId, key := range keysByKeyId {
        if "" == keyId {
            exception.Panic(exception.NewError("hmac key id is empty", map[string]any{"field": "HmacKeyRing.Keys"}, nil))
        }

        if "" == key.Application {
            exception.Panic(
                exception.NewError("hmac key has no application", map[string]any{"field": "HmacKeyRing.Keys", "keyId": keyId}, nil),
            )
        }

        if 0 == len(key.Secret) {
            exception.Panic(
                exception.NewError("hmac secret is empty", map[string]any{"field": "HmacKeyRing.Keys", "keyId": keyId}, nil),
            )
        }

        if minimumSecretLength > len(key.Secret) {
            shortSecret := shortSecretContext("field", "HmacKeyRing.Keys", len(key.Secret), minimumSecretLength)
            shortSecret["keyId"] = keyId

            exception.Panic(
                exception.NewError("hmac secret is shorter than its minimum, the 32 bytes RFC 7518 asks of an HMAC-SHA256 key unless MinimumSecretLength raises it", shortSecret, nil),
            )
        }

        /* the key id is attacker-visible, so a shared secret would sign under either application */
        if owner, reused := applicationBySecret[string(key.Secret)]; true == reused && owner != key.Application {
            exception.Panic(
                exception.NewError(
                    "hmac secret is reused across applications",
                    map[string]any{"field": "HmacKeyRing.Keys", "keyId": keyId, "application": key.Application, "otherApplication": owner},
                    nil,
                ),
            )
        }
        applicationBySecret[string(key.Secret)] = key.Application

        secretsByKeyId[keyId] = append([]byte{}, key.Secret...)
        applicationsByKeyId[keyId] = key.Application
    }

    if _, exists := secretsByKeyId[currentKeyId]; false == exists {
        exception.Panic(
            exception.NewError(
                "hmac current key id has no secret",
                map[string]any{"field": "HmacKeyRing.CurrentKeyId", "keyId": currentKeyId},
                nil,
            ),
        )
    }

    return &StaticHmacSecretProvider{
        currentKeyId:        currentKeyId,
        secretsByKeyId:      secretsByKeyId,
        applicationsByKeyId: applicationsByKeyId,
    }
}

type StaticHmacSecretProvider struct {
    currentKeyId        string
    secretsByKeyId      map[string][]byte
    applicationsByKeyId map[string]string
}

func (instance *StaticHmacSecretProvider) CurrentKeyId(runtimeInstance containercontract.Runtime) (string, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance, "security.StaticHmacSecretProvider.CurrentKeyId"); nil != runtimeErr {
        return "", runtimeErr
    }

    return instance.currentKeyId, nil
}

func (instance *StaticHmacSecretProvider) Secret(runtimeInstance containercontract.Runtime, keyId string) ([]byte, bool, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance, "security.StaticHmacSecretProvider.Secret"); nil != runtimeErr {
        return nil, false, runtimeErr
    }

    secret, exists := instance.secretsByKeyId[keyId]
    if false == exists {
        return nil, false, nil
    }

    return append([]byte{}, secret...), true, nil
}

func (instance *StaticHmacSecretProvider) ApplicationForKeyId(runtimeInstance containercontract.Runtime, keyId string) (string, bool, error) {
    if runtimeErr := containercontract.RefuseNilRuntime(runtimeInstance, "security.StaticHmacSecretProvider.ApplicationForKeyId"); nil != runtimeErr {
        return "", false, runtimeErr
    }

    application, exists := instance.applicationsByKeyId[keyId]
    if false == exists {
        return "", false, nil
    }

    return application, true, nil
}

var _ HmacSecretProvider = (*StaticHmacSecretProvider)(nil)
