package cli

import (
    "fmt"
    "time"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security/totp"
)

/* NewTotpCodeCommand prints the current TOTP code for a secret — it stands in for the authenticator app a
user would hold, so the two-factor verification can be driven end-to-end. */
func NewTotpCodeCommand() *TotpCodeCommand {
    return &TotpCodeCommand{}
}

type TotpCodeCommand struct{}

func (instance *TotpCodeCommand) Name() string {
    return "totp:code"
}

func (instance *TotpCodeCommand) Description() string {
    return "prints the current TOTP code for a secret (stands in for an authenticator app)"
}

func (instance *TotpCodeCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:  "secret",
            Usage: "the base32 TOTP secret returned by the enrollment endpoint",
        },
    }
}

func (instance *TotpCodeCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    secret := commandContext.String("secret")
    if "" == secret {
        return fmt.Errorf("a --secret is required")
    }

    code, codeErr := totp.GenerateCodeAt(secret, time.Now(), totp.Config{})
    if nil != codeErr {
        return codeErr
    }

    fmt.Printf("%s\n", code)

    return nil
}
