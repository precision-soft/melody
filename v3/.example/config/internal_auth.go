package config

import (
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

const (
    internalCallerKeyId  = "wms-key-1"
    internalCallerApp    = "wms-service"
    internalCallerSecret = "melody-example-internal-shared-secret-0001"
    internalCallerRole   = "ROLE_SERVICE"
)

func (instance *Module) buildInternalAuth() {
    instance.hmacSecrets = melodysecurity.NewStaticHmacSecretProvider(
        internalCallerKeyId,
        map[string]melodysecurity.HmacKey{
            internalCallerKeyId: {App: internalCallerApp, Secret: []byte(internalCallerSecret)},
        },
    )

    instance.hmacApps = melodysecurity.NewStaticHmacAppRegistry(
        map[string][]string{
            internalCallerApp: {internalCallerRole},
        },
    )
}

func (instance *Module) internalAuthSigner() *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App:     internalCallerApp,
        Secrets: instance.hmacSecrets,
    })
}
