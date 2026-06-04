package config

import (
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

const demoJwtSecret = "melody-example-demo-secret-change-me"

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(demoJwtSecret)
    instance.tokenValidator = melodysecurity.NewJwtTokenValidator(
        melodysecurity.JwtConfig{
            Secret: instance.jwtSecret,
        },
    )
}
