package cli

import (
    "fmt"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* GrantDemoCommand declares its own --role flag to show that an application command may reuse a name the
runtime also understands: the runtime's --mode/--role are recognized only before the command name, so
`example:grant:demo --role admin` reaches this command intact rather than being captured (and rejected) by
the process-role parser. It also holds the user service through a container.Lazy handle built at
command-registration time — the service is resolved at the first run, not when the command is constructed,
so the boot-phase composition never resolves the container early. */
type GrantDemoCommand struct {
    userService *melodycontainer.LazyService[*service.UserService]
}

func NewGrantDemoCommand(userService *melodycontainer.LazyService[*service.UserService]) *GrantDemoCommand {
    return &GrantDemoCommand{userService: userService}
}

func (instance *GrantDemoCommand) Name() string {
    return "example:grant:demo"
}

func (instance *GrantDemoCommand) Description() string {
    return "grants an application role to a user (demonstrates a command-owned --role flag and a lazily-resolved service)"
}

func (instance *GrantDemoCommand) Flags() []melodyclicontract.Flag {
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

func (instance *GrantDemoCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext *melodyclicontract.CommandContext) error {
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

    _, known, findErr := userService.FindByUsername(user)
    if nil != findErr {
        return findErr
    }

    fmt.Printf("user service resolved lazily: user %q known=%t\n", user, known)
    fmt.Printf("granted role %q to user %q\n", role, user)

    return nil
}

var _ melodyclicontract.Command = (*GrantDemoCommand)(nil)
