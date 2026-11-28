package config

import (
    "time"

    "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/bound"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

const (
    /* the development value of APP_JWT_SECRET, the one .env commits: public, so outside development it refuses the boot */
    exampleJwtSecret = "melody-example-signing-secret-change-me"

    /* tokenRevocationEpochSkew widens a revoke-all two seconds past the instant it was drawn: the replicas behind the balancer issue and revoke on clocks that may differ by that much, so a token another node stamped just after the revoke, on a clock behind this one's, is still refused; the price is that a sign-in inside those two seconds signs in once more */
    tokenRevocationEpochSkew = 2 * time.Second
)

/* exampleJwtKeySet is the one HS256 secret the token firewall verifies bearer tokens with; a rotating or remote key source implements securitycontract.JwtKeySet instead */
func exampleJwtKeySet(secret []byte) *melodysecurity.JwtSecretKeySet {
    return melodysecurity.NewJwtSecretKeySet(secret, melodysecurity.JwtSecretKeySetConfig{})
}

/* exampleJwtConfig is the configuration the token firewall verifies bearer tokens with */
func exampleJwtConfig() melodysecurity.JwtConfig {
    return melodysecurity.JwtConfig{
        ScopeClaim:          "scope",
        DeviceClaim:         security.DeviceClaim,
        RevocationEpochSkew: bound.Of(tokenRevocationEpochSkew),
    }
}

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(instance.credential(environmentKeyJwtSecret, parameterJwtSecret, exampleJwtSecret))
    jwtConfig := exampleJwtConfig()
    jwtConfig.RevocationEpochStore = security.ResolvedTokenStore{}
    instance.tokenValidator = melodysecurity.NewJwtTokenValidator(exampleJwtKeySet(instance.jwtSecret), jwtConfig)

    instance.opaqueTokenStore = melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{})

    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(security.ResolvedTokenStore{})
}
