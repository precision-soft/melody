package cli

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* GrantRoleCommand writes an application role to an existing account. Its --role flag follows the command name; process --role precedes it. The user service is resolved lazily on the first run. */
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
    role := strings.TrimSpace(commandContext.String("role"))
    user := strings.TrimSpace(commandContext.String("user"))
    if "" == role { return fmt.Errorf("no role given; pass --role to grant one") }
    if "" == user { return fmt.Errorf("no user given; pass --user to select one") }
    if strings.Contains(role, ",") { return fmt.Errorf("role %q must not contain commas", role) }
    userService, resolveErr := instance.userService.Resolve()
    if nil != resolveErr { return resolveErr }
    account, changed, err := userService.GrantRole(runtimeInstance, user, role)
    if nil != err { return err }
    fmt.Printf("user service resolved lazily: user %q known=%t\n", user, nil != account)
    if nil == account { return fmt.Errorf("user %q does not exist", user) }
    if false == changed {
        fmt.Printf("user %q already holds role %q; nothing to do\n", user, role)
        return nil
    }

    fmt.Printf("granted role %q to user %q\n", role, user)

    if true == cacheIsProcessLocal(runtimeInstance) {
        fmt.Println(processLocalCacheNotice)
    }

    return nil
}

func holdsRole(roles []string, role string) bool {
    for _, held := range roles {
        if role == held {
            return true
        }
    }

    return false
}

var _ melodyclicontract.Command = (*GrantRoleCommand)(nil)
