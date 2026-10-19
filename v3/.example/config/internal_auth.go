package config

import (
    "time"

    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* the internal-auth wiring mirrors the cross-app machine-to-machine story: a caller service (wms-service) signs an HMAC envelope with a shared secret bound to its own key id, and this application verifies it and authenticates the call as the caller's principal (roles from the app registry). The three are configuration — APP_INTERNAL_AUTH_KEY_ID, APP_INTERNAL_AUTH_APP and APP_INTERNAL_AUTH_SECRET —, and the values below are the ones .env commits: a blank key id or application takes them anywhere, since they name the caller and grant nothing, while the secret is a credential and outside development refuses the boot blank or committed. */
const (
    internalCallerKeyId  = "wms-key-1"
    internalCallerApp    = "wms-service"
    internalCallerSecret = "melody-example-internal-shared-secret-0001"
    internalCallerRole   = "ROLE_SERVICE"

    /* internalEnvelopeMaxFutureExpiry is how far past this process's clock an envelope's expiry may sit: the nonce guard remembers each nonce until its envelope expires, so an unbounded horizon lets a caller make the replay memory hold a nonce for as long as it likes */
    internalEnvelopeMaxFutureExpiry = 5 * time.Minute
)

func (instance *Module) buildInternalAuth() {
    keyId := instance.environmentValueOr(environmentKeyInternalAuthKeyId, internalCallerKeyId)
    instance.internalAuthApp = instance.environmentValueOr(environmentKeyInternalAuthApp, internalCallerApp)
    secret := instance.credential(environmentKeyInternalAuthSecret, internalCallerSecret)

    instance.hmacSecrets = melodysecurity.NewStaticHmacSecretProvider(
        keyId,
        map[string]melodysecurity.HmacKey{
            keyId: {App: instance.internalAuthApp, Secret: []byte(secret)},
        },
    )

    instance.hmacApps = melodysecurity.NewStaticHmacAppRegistry(
        map[string][]string{
            instance.internalAuthApp: {internalCallerRole},
        },
    )
}

/* internalAuthSigner builds a signer for the caller service (wms-service), used by the internal:sign CLI command to mint a header a client would send, valid for ttl, zero taking the signer's default. It shares the same secret provider the firewall verifies against, so a signed envelope authenticates. */
func (instance *Module) internalAuthSigner(ttl time.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App:     instance.internalAuthApp,
        Secrets: instance.hmacSecrets,
        Ttl:     ttl,
    })
}
