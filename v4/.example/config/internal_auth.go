package config

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

/* the internal-auth wiring mirrors the cross-app machine-to-machine story: a caller service (wms-service) signs an HMAC envelope with a shared secret bound to its own key id, and this application verifies it and authenticates the call as the caller's principal (roles from the application registry). The three are configuration — APP_INTERNAL_AUTH_KEY_ID, APP_INTERNAL_AUTH_APP and APP_INTERNAL_AUTH_SECRET —, and the values below are the ones .env commits: a blank key id or application takes them anywhere, since they name the caller and grant nothing, while the secret is a credential and outside development refuses the boot blank or committed. */
const (
    internalCallerKeyId       = "wms-key-1"
    internalCallerApplication = "wms-service"
    internalCallerSecret      = "melody-example-internal-shared-secret-0001"
    internalCallerRole        = "ROLE_SERVICE"

    /* internalEnvelopeMaxFutureExpiry is how far past this process's clock an envelope's expiry may sit: the nonce guard remembers each nonce until its envelope expires, so an unbounded horizon lets a caller make the replay memory hold a nonce for as long as it likes */
    internalEnvelopeMaxFutureExpiry = 5 * time.Minute
)

func (instance *Module) buildInternalAuth() {
    keyId := instance.environmentValueOr(environmentKeyInternalAuthKeyId, internalCallerKeyId)
    instance.internalAuthApplication = instance.environmentValueOr(environmentKeyInternalAuthApplication, internalCallerApplication)
    secret := instance.credential(environmentKeyInternalAuthSecret, internalCallerSecret)

    instance.hmacSecrets = melodysecurity.NewStaticHmacSecretProvider(melodysecurity.HmacKeyRing{
        CurrentKeyId: keyId,
        Keys: map[string]melodysecurity.HmacKey{
            keyId: {Application: instance.internalAuthApplication, Secret: []byte(secret)},
        },
    })

    instance.hmacApplications = melodysecurity.NewStaticHmacApplicationRegistry(
        map[string][]string{
            instance.internalAuthApplication: {internalCallerRole},
        },
    )
}

/* internalAuthSigner builds a signer for the caller service (wms-service), used by the internal:sign CLI command to mint a header a client would send, valid for ttl, an absent one taking the signer's default. It shares the same secret provider the firewall verifies against, so a signed envelope authenticates. */
func (instance *Module) internalAuthSigner(ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        Application: instance.internalAuthApplication,
        Secrets:     instance.hmacSecrets,
        Ttl:         ttl,
    })
}
