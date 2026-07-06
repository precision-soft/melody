package config

import (
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* the internal-auth demo mirrors the cross-app machine-to-machine story: a caller service (wms-service)
signs an HMAC envelope with a shared secret bound to its own key id, and this application verifies it and
authenticates the call as the wms-service principal (roles from the app registry). The secret is a demo
value — a real deployment sources it from configuration/secrets management and rotates it. */
const (
    demoInternalKeyId  = "wms-key-1"
    demoInternalApp    = "wms-service"
    demoInternalSecret = "melody-example-internal-shared-secret-0001"
    demoInternalRole   = "ROLE_SERVICE"
)

func (instance *Module) buildInternalAuth() {
    instance.hmacSecrets = melodysecurity.NewStaticHmacSecretProvider(
        demoInternalKeyId,
        map[string]melodysecurity.HmacKey{
            demoInternalKeyId: {App: demoInternalApp, Secret: []byte(demoInternalSecret)},
        },
    )

    instance.hmacApps = melodysecurity.NewStaticHmacAppRegistry(
        map[string][]string{
            demoInternalApp: {demoInternalRole},
        },
    )
}

/* internalAuthSigner builds a signer for the demo caller (wms-service), used by the internal:sign CLI
command to mint a header a client would send. It shares the same secret provider the firewall verifies
against, so a signed envelope authenticates. */
func (instance *Module) internalAuthSigner() *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(melodysecurity.HmacEnvelopeSignerConfig{
        App:     demoInternalApp,
        Secrets: instance.hmacSecrets,
    })
}
