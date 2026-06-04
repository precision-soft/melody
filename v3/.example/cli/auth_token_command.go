package cli

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "time"

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
    return "mints a demo HS256 JWT for the token-protected secure api"
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
        &melodyclicontract.IntFlag{
            Name:  "ttl",
            Usage: "token lifetime in seconds; defaults to 3600",
        },
    }
}

func (instance *AuthTokenCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    user := commandContext.String("user")
    if "" == user {
        user = "demo-user"
    }

    roles := commandContext.StringSlice("role")
    if 0 == len(roles) {
        roles = []string{"ROLE_USER"}
    }

    ttl := int64(commandContext.Int("ttl"))
    if 0 >= ttl {
        ttl = 3600
    }

    now := time.Now()
    claims := map[string]any{
        "sub":   user,
        "roles": roles,
        "iat":   now.Unix(),
        "exp":   now.Add(time.Duration(ttl) * time.Second).Unix(),
    }

    fmt.Println(signHs256(instance.secret, claims))

    return nil
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
