package cli

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/service"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* TwoFactorResetCommand removes the second factor of the account named as its argument, secret and recovery codes together. The enrollment door replaces a factor only for a request carrying a current one, so an account whose authenticator and recovery codes are both lost — the last code spent at a sign-in — comes back through an operator here, never through a session alone; after the reset the account signs in with its password and enrolls again. */
type TwoFactorResetCommand struct {
    userService *melodycontainer.LazyService[*service.UserService]
    storeSource twofactor.StoreSource
}

func NewTwoFactorResetCommand(userService *melodycontainer.LazyService[*service.UserService], storeSource twofactor.StoreSource) *TwoFactorResetCommand {
    return &TwoFactorResetCommand{userService: userService, storeSource: storeSource}
}

func (instance *TwoFactorResetCommand) Name() string {
    return "example:twofactor:reset"
}

func (instance *TwoFactorResetCommand) Description() string {
    return "removes the second factor of the user named as the argument, for an account whose authenticator and recovery codes are both lost"
}

func (instance *TwoFactorResetCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *TwoFactorResetCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    writer := commandContext.Writer()

    arguments := commandContext.Arguments()
    if 1 != len(arguments) {
        return fmt.Errorf("name exactly one user whose second factor to remove, as the argument (got %d)", len(arguments))
    }

    username := strings.TrimSpace(arguments[0])

    userService, resolveErr := instance.userService.Resolve()
    if nil != resolveErr {
        return resolveErr
    }

    account, known, findErr := userService.FindByUsername(username)
    if nil != findErr {
        return findErr
    }

    if false == known {
        return fmt.Errorf("user %q does not exist", username)
    }

    store, storeErr := instance.storeSource(runtimeInstance)
    if nil != storeErr {
        return storeErr
    }

    removed, deleteErr := store.DeleteEnrollment(runtimeInstance, account.Id)
    if nil != deleteErr {
        return deleteErr
    }

    if false == removed {
        _, _ = fmt.Fprintf(writer, "user %q holds no second factor; nothing to do\n", username)

        return nil
    }

    _, _ = fmt.Fprintf(writer, "removed the second factor of user %q; the account signs in with its password and enrolls again\n", username)

    return nil
}

var _ melodyclicontract.Command = (*TwoFactorResetCommand)(nil)
