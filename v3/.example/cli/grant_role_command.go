package cli

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* GrantRoleCommand grants an application role to an account.

   It declares its own --role flag to show that an application command may reuse a name the runtime also understands: the runtime's --mode/--role are recognized only before the command name, so `example:grant:role --role admin` reaches this command intact rather than being captured (and rejected) by the process-role parser. It also holds the user service through a container.Lazy handle built at command-registration time — the service is resolved at the first run, not when the command is constructed, so the boot-phase composition never resolves the container early.

   The grant is a real write. It used to be a print: the command looked the account up and then announced "granted role ... to user ...", for an account it had just been told did not exist as readily as for one it had found, and left the directory untouched. */
type GrantRoleCommand struct {
    userService *melodycontainer.LazyService[*service.UserService]
}

func NewGrantRoleCommand(userService *melodycontainer.LazyService[*service.UserService]) *GrantRoleCommand {
    return &GrantRoleCommand{userService: userService}
}

func (instance *GrantRoleCommand) Name() string {
    return "example:grant:role"
}

func (instance *GrantRoleCommand) Description() string {
    return "grants an application role to a user (demonstrates a command-owned --role flag and a lazily-resolved service)"
}

func (instance *GrantRoleCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:  "role",
            Usage: "the application role to grant (this is the command's own flag, not the runtime process role)",
        },
        &melodyclicontract.StringFlag{
            Name:  "user",
            Usage: "the user to grant the role to",
        },
    }
}

func (instance *GrantRoleCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    role := commandContext.String("role")
    user := commandContext.String("user")

    if "" == role {
        fmt.Println("no role given; pass --role to grant one")

        return nil
    }

    /* first use: the lazy handle resolves the user service now and memoizes the success for later runs in the same process. */
    userService, resolveErr := instance.userService.Resolve()
    if nil != resolveErr {
        return resolveErr
    }

    if true == strings.Contains(role, ",") {
        /* the roles column is one comma-joined value, so a role carrying a comma comes back as several on the next read — the same refusal the two admin doors make, at the only other door that writes roles */
        return fmt.Errorf("role %q must not contain commas", role)
    }

    account, known, findErr := userService.FindByUsername(user)
    if nil != findErr {
        return findErr
    }

    fmt.Printf("user service resolved lazily: user %q known=%t\n", user, known)

    if false == known {
        return fmt.Errorf("user %q does not exist", user)
    }

    if true == holdsRole(account.Roles, role) {
        fmt.Printf("user %q already holds role %q; nothing to do\n", user, role)

        return nil
    }

    /* the role is ADDED to the list the account holds, never substituted for it: the update door takes the whole set, so passing the one role would strip every other. The stored digest travels back unchanged for the same reason. */
    _, updated, updateErr := userService.Update(
        runtimeInstance,
        account.Id,
        account.Username,
        account.Password,
        append(append([]string{}, account.Roles...), role),
    )
    if nil != updateErr {
        return updateErr
    }

    if false == updated {
        return fmt.Errorf("user %q disappeared before the role could be granted", user)
    }

    fmt.Printf("granted role %q to user %q\n", role, user)

    return nil
}

/* holdsRole answers whether the account already carries the role, so a second run of the same command is a no-op rather than a second entry in the column and a second line in the audit trail. */
func holdsRole(roles []string, role string) bool {
    for _, held := range roles {
        if role == held {
            return true
        }
    }

    return false
}

var _ melodyclicontract.Command = (*GrantRoleCommand)(nil)
