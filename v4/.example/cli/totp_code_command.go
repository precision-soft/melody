package cli

import (
    "errors"
    "fmt"
    "io"

    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/security/totp"
)

const totpSecretMaximumBytes = 256

/* NewTotpCodeCommand prints the current TOTP code for a secret read from the first line of its input — standard input in the wiring, never an argument, where the shell history and the process list would keep it; it stands in for the authenticator app a user would hold, so the two-factor verification can be driven end-to-end. */
func NewTotpCodeCommand(secretInput io.Reader) *TotpCodeCommand {
    return &TotpCodeCommand{secretInput: secretInput}
}

type TotpCodeCommand struct {
    secretInput io.Reader
}

func (instance *TotpCodeCommand) Name() string {
    return "totp:code"
}

func (instance *TotpCodeCommand) Description() string {
    return "prints the current TOTP code for the base32 secret read from standard input, never from an argument (stands in for an authenticator app): printf '%s' \"$SECRET\" | ./app totp:code"
}

func (instance *TotpCodeCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *TotpCodeCommand) Run(
    runtimeInstance melodycontainercontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    if nil == instance.secretInput {
        return errors.New("the secret is read from standard input, and this command has none")
    }

    secret, readErr := readFirstLine(runtimeInstance.Context(), instance.secretInput, totpSecretMaximumBytes, "totp secret: ")
    if true == errors.Is(readErr, errFirstLineTooLong) {
        return fmt.Errorf("the secret is longer than %d bytes", totpSecretMaximumBytes)
    }

    if nil != readErr {
        return fmt.Errorf("reading the secret from standard input: %w", readErr)
    }

    if "" == secret {
        return errors.New("a secret is required on standard input")
    }

    clockInstance, clockErr := melodyruntime.FromRuntime[melodyclockcontract.Clock](runtimeInstance, melodyclock.ServiceClock)
    if nil != clockErr {
        return clockErr
    }

    code, codeErr := totp.GenerateCodeAt(secret, clockInstance.Now(), totp.Config{})
    if nil != codeErr {
        return codeErr
    }

    _, writeErr := fmt.Fprintf(commandContext.Writer, "%s\n", code)

    return writeErr
}

var _ melodyclicontract.Command = (*TotpCodeCommand)(nil)
