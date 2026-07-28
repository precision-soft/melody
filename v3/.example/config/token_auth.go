package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

const (
    exampleJwtSecret   = "melody-example-signing-secret-change-me"
    exampleOpaqueToken = "melody-example-opaque-token"
)

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(exampleJwtSecret)
    instance.tokenValidator = melodysecurity.NewJwtTokenValidator(
        melodysecurity.JwtConfig{
            Secret:     instance.jwtSecret,
            ScopeClaim: "scope",
        },
    )

    opaqueStore := melodysecurity.NewInMemoryTokenStore()
    opaqueStore.Put(exampleOpaqueToken, melodysecuritycontract.Claims{
        UserIdentifier: "api-user",
        Roles:          []string{entity.RoleUser},
    })

    instance.opaqueTokenStore = opaqueStore
    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(opaqueStore)
}
