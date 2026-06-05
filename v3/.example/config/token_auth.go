package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

const (
    demoJwtSecret   = "melody-example-demo-secret-change-me"
    demoOpaqueToken = "melody-example-opaque-token"
)

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(demoJwtSecret)
    instance.tokenValidator = melodysecurity.NewJwtTokenValidator(
        melodysecurity.JwtConfig{
            Secret: instance.jwtSecret,
        },
    )

    /** A revocable alternative to the stateless JWT validator: an in-memory store seeded with a
    demo token resolves to fixed claims and can be revoked at runtime via store.Delete /
    DeleteByUser. Swap instance.opaqueTokenValidator into RegisterSecurity's stateless firewall in
    place of instance.tokenValidator to authenticate with opaque bearer tokens instead. */
    opaqueStore := melodysecurity.NewInMemoryTokenStore()
    opaqueStore.Put(demoOpaqueToken, melodysecuritycontract.Claims{
        UserIdentifier: "api-user",
        Roles:          []string{entity.RoleUser},
    })

    instance.opaqueTokenStore = opaqueStore
    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(opaqueStore)
}
