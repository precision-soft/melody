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
            /** copies the `scope` object claim into Claims.Scope so the enricher can resolve roles from it */
            ScopeClaim: "scope",
        },
    )

    opaqueStore := melodysecurity.NewInMemoryTokenStore()
    opaqueStore.Put(demoOpaqueToken, melodysecuritycontract.Claims{
        UserIdentifier: "api-user",
        Roles:          []string{entity.RoleUser},
    })

    instance.opaqueTokenStore = opaqueStore
    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(opaqueStore)
}
