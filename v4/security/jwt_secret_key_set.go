package security

import (
    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type JwtSecretKeySetConfig struct {
    /* MinimumSecretLength is the fewest bytes the secret may hold: 32 by default, the RFC 7518 floor for an HS256 key, at least 32, unlimited refused; a shorter secret is refused naming the secret argument. */
    MinimumSecretLength bound.Count
}

/* NewJwtSecretKeySet builds the key set of one HS256 secret, panicking on an empty secret, one shorter than MinimumSecretLength and a minimum its rule refuses. */
func NewJwtSecretKeySet(secret []byte, config JwtSecretKeySetConfig) *JwtSecretKeySet {
    if 0 == len(secret) {
        exception.Panic(exception.NewError("jwt secret is empty", exceptioncontract.Context{"argument": "secret"}, nil))
    }

    minimumSecretLength := resolveMinimumSecretLength(config.MinimumSecretLength, "JwtSecretKeySetConfig.MinimumSecretLength")
    if minimumSecretLength > len(secret) {
        exception.Panic(exception.NewError(
            "jwt secret is shorter than its minimum, the 32 bytes RFC 7518 asks of an HS256 key unless MinimumSecretLength raises it",
            shortSecretContext("argument", "secret", len(secret), minimumSecretLength),
            nil,
        ))
    }

    return &JwtSecretKeySet{
        /* the secret is copied on the way in, so the caller's slice cannot change it under a later signature check */
        secret: append([]byte{}, secret...),
    }
}

/* JwtSecretKeySet answers its one HS256 key whatever kid a token names. */
type JwtSecretKeySet struct {
    secret []byte
}

func (instance *JwtSecretKeySet) VerificationKeys(
    runtimeInstance containercontract.Runtime,
    query securitycontract.JwtKeyQuery,
) ([]securitycontract.JwtKey, error) {
    /* a copy per answer, so a caller of the door cannot change the secret every later token is verified with */
    return []securitycontract.JwtKey{{KeyId: "", Algorithm: JwtAlgorithmHs256, Secret: append([]byte{}, instance.secret...)}}, nil
}

var _ securitycontract.JwtKeySet = (*JwtSecretKeySet)(nil)
