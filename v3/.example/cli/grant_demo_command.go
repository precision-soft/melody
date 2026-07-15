package cli

import (
    "fmt"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* GrantDemoCommand declares its own --role flag to show that an application command may reuse a name the
runtime also understands: the runtime's --mode/--role are recognized only before the command name, so
`example:grant:demo --role admin` reaches this command intact rather than being captured (and rejected) by
the process-role parser. */
type GrantDemoCommand struct{}

func NewGrantDemoCommand() *GrantDemoCommand {
    return &GrantDemoCommand{}
}

func (instance *GrantDemoCommand) Name() string {
    return "example:grant:demo"
}

func (instance *GrantDemoCommand) Description() string {
    return "grants an application role to a user (demonstrates a command-owned --role flag)"
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

    fmt.Printf("granted role %q to user %q\n", role, user)

    return nil
}

var _ melodyclicontract.Command = (*GrantDemoCommand)(nil)
