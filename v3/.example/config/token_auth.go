package config

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/security"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

const (
    /* the development value of APP_JWT_SECRET, the one .env commits: public, so outside development it refuses the boot */
    exampleJwtSecret = "melody-example-signing-secret-change-me"

    /* tokenRevocationEpochSkew widens a revoke-all two seconds past the instant it was drawn: the replicas behind the balancer issue and revoke on clocks that may differ by that much, so a token another node stamped just after the revoke, on a clock behind this one's, is still refused; the price is that a sign-in inside those two seconds signs in once more */
    tokenRevocationEpochSkew = 2 * time.Second
)

/* exampleJwtConfig is the configuration the token firewall verifies bearer tokens with */
func exampleJwtConfig(secret []byte) melodysecurity.JwtConfig {
    return melodysecurity.JwtConfig{
        Secret:              secret,
        ScopeClaim:          "scope",
        DeviceClaim:         security.DeviceClaim,
        RevocationEpochSkew: tokenRevocationEpochSkew,
    }
}

func (instance *Module) buildTokenAuth() {
    instance.jwtSecret = []byte(instance.credential(environmentKeyJwtSecret, exampleJwtSecret))
    instance.tokenValidator = melodysecurity.NewJwtTokenValidatorWithRevocationEpoch(
        exampleJwtConfig(instance.jwtSecret),
        security.ResolvedTokenStore{},
    )

    instance.opaqueTokenStore = melodysecurity.NewInMemoryTokenStore()

    instance.opaqueTokenValidator = melodysecurity.NewOpaqueTokenValidator(security.ResolvedTokenStore{})
}
