package cli

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "math"
    "time"

    examplesecurity "github.com/precision-soft/melody/v3/.example/security"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewAuthTokenCommand(secret []byte) *AuthTokenCommand {
    return &AuthTokenCommand{
        secret: secret,
    }
}

type AuthTokenCommand struct {
    secret []byte
}

func (instance *AuthTokenCommand) Name() string {
    return "auth:token"
}

func (instance *AuthTokenCommand) Description() string {
    return "mints an HS256 JWT for the token-protected secure api"
}

func (instance *AuthTokenCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:  "user",
            Usage: "subject (user identifier) embedded in the token",
        },
        &melodyclicontract.StringSliceFlag{
            Name:  "role",
            Usage: "role embedded in the token (repeat for multiple)",
        },
        &melodyclicontract.StringSliceFlag{
            Name:  "scope-role",
            Usage: "role carried in the token's scope claim, the way an OAuth-style issuer grants it (repeat for multiple); given alone, the roles claim is left empty",
        },
        &melodyclicontract.StringFlag{
            Name:  "device",
            Usage: "device the token is issued to; a revocation can then end this device without ending the others",
        },
        &melodyclicontract.IntFlag{
            Name:  "ttl",
            Usage: "token lifetime in seconds; defaults to 3600",
        },
    }
}

func (instance *AuthTokenCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext melodyclicontract.Context,
) error {
    user := commandContext.String("user")
    if "" == user {
        user = "example-user"
    }

    /* the scope roles are the issuer's grant, read by the firewall's scope enricher; a token that names only them carries an empty roles claim, so the role it is granted is the scope's and nothing else */
    roles := commandContext.StringSlice("role")
    scopeRoles := commandContext.StringSlice("scope-role")
    if 0 == len(roles) && 0 == len(scopeRoles) {
        roles = []string{"ROLE_USER"}
    }

    if nil == roles {
        roles = []string{}
    }

    ttl := int64(commandContext.Int("ttl"))
    if 0 >= ttl {
        ttl = 3600
    }

    /* the seconds-to-nanoseconds multiplication below wraps for a ttl past MaxInt64 nanoseconds (~292 years), minting a token whose exp sits centuries in the PAST — refused by every verifier as expired the moment it is printed. Saturating keeps an absurdly large ttl meaning "far future". */
    const maxTtlSeconds = math.MaxInt64 / int64(time.Second)
    if maxTtlSeconds < ttl {
        ttl = maxTtlSeconds
    }

    now := time.Now()
    claims := map[string]any{
        "sub":   user,
        "roles": roles,
        "iat":   now.Unix(),
        "exp":   now.Add(time.Duration(ttl) * time.Second).Unix(),
    }

    if 0 < len(scopeRoles) {
        claims["scope"] = map[string]any{"roles": scopeRoles}
    }

    if device := commandContext.String("device"); "" != device {
        claims[examplesecurity.DeviceClaim] = device
    }

    _, writeErr := fmt.Fprintln(commandContext.Writer(), signHs256(instance.secret, claims))

    return writeErr
}

func signHs256(secret []byte, claims map[string]any) string {
    headerJson, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

var _ melodyclicontract.Command = (*AuthTokenCommand)(nil)
