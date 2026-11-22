package config

import (
    "strings"
    "time"

    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

/* ServiceInternalAuthSigner is the caller service's signer, built once at boot with the signer's default envelope lifetime. */
const ServiceInternalAuthSigner = "service.example.internal_auth_signer"

/* a blank key id or application takes these; the secret is a credential and refuses the boot outside development */
const (
    internalCallerKeyId       = "wms-key-1"
    internalCallerApplication = "wms-service"
    internalCallerSecret      = "melody-example-internal-shared-secret-0001"
    internalCallerRole        = "ROLE_SERVICE"

    /* the nonce guard holds each nonce until its envelope expires, so the horizon bounds that memory */
    internalEnvelopeMaxFutureExpiry = 5 * time.Minute
)

func (instance *Module) buildInternalAuth() {
    keyId := strings.TrimSpace(instance.parameterValue(parameterInternalAuthKeyId))
    instance.internalAuthApplication = strings.TrimSpace(instance.parameterValue(parameterInternalAuthApplication))
    secret := instance.credential(environmentKeyInternalAuthSecret, parameterInternalAuthSecret, internalCallerSecret)

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

func (instance *Module) registerInternalAuthSignerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        ServiceInternalAuthSigner,
        func(resolver melodycontainercontract.Resolver) (*melodysecurity.HmacEnvelopeSigner, error) {
            return instance.internalAuthSigner(resolver.Runtime(), bound.Duration{}), nil
        },
        melodycontainer.RegisterOptions{BuiltAtBoot: true, BootFailure: melodycontainer.BootFailureFailsBoot},
    )
}

func (instance *Module) internalAuthSigner(runtimeInstance melodycontainercontract.Runtime, ttl bound.Duration) *melodysecurity.HmacEnvelopeSigner {
    return melodysecurity.NewHmacEnvelopeSigner(runtimeInstance, instance.hmacSecrets, melodysecurity.HmacEnvelopeSignerConfig{
        Application: instance.internalAuthApplication,
        Ttl:         ttl,
    })
}
