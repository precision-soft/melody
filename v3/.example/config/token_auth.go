package config

import (
    "github.com/precision-soft/melody/v3/.example/security"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

const exampleJwtSecret = "melody-example-signing-secret-change-me"

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(exampleJwtSecret)
    instance.tokenValidator = melodysecurity.NewJwtTokenValidatorWithRevocationEpoch(
        melodysecurity.JwtConfig{
            Secret:      instance.jwtSecret,
            ScopeClaim:  "scope",
            DeviceClaim: security.DeviceClaim,
        },
        security.ResolvedTokenStore{},
    )

    instance.opaqueTokenStore = melodysecurity.NewInMemoryTokenStore()

    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(security.ResolvedTokenStore{})
}
