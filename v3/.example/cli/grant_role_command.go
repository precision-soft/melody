package cli

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* GrantRoleCommand grants an application role to an account. Its own --role flag shows that an application command may reuse a name the runtime understands, since the runtime's --mode/--role are recognized only before the command name. It holds the user service through a container.Lazy handle, resolved at the first run rather than when the command is constructed. */
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
    /* the flag is trimmed and judged against the roles the application knows before anything is read, as the two admin doors normalise what they store: an untrimmed role would never match the no-op check and be appended on every re-run, and a misspelt one would be reported as granted while the voter, which compares exactly, grants nothing */
    role := strings.TrimSpace(commandContext.String("role"))
    user := commandContext.String("user")

    if "" == role {
        fmt.Println("no role given; pass --role to grant one")

        return nil
    }

    if true == strings.Contains(role, ",") {
        /* the roles column is one comma-joined value, so a role carrying a comma comes back as several on the next read — the same refusal the two admin doors make, at the only other door that writes roles */
        return fmt.Errorf("role %q must not contain commas", role)
    }

    if false == entity.IsKnownRole(role) {
        return fmt.Errorf("role %q is not one this application knows (%s)", role, strings.Join(entity.KnownRoleList(), ", "))
    }

    /* first use: the lazy handle resolves the user service now and memoizes the success for later runs in the same process. */
    userService, resolveErr := instance.userService.Resolve()
    if nil != resolveErr {
        return resolveErr
    }

    account, known, findErr := userService.FindByUsername(user)
    if nil != findErr {
        return findErr
    }

    fmt.Printf("user service resolved lazily: user %q known=%t\n", user, known)

    if false == known {
        return fmt.Errorf("user %q does not exist", user)
    }

    /* the role is added through the repository's atomic door, which reads and writes the account under one lock, so a grant beside an admin update of the same account cannot lose either change. The repository alone decides whether the role is already held, because the read above may come from a cache. */
    granted, outcome, grantErr := userService.GrantRole(runtimeInstance, account.Id, role)
    if nil != grantErr {
        /* a grant whose write committed and whose listeners then refused is not a failed grant: the role is in the directory and the account's cache entries are not dropped, and the operator reads both rather than re-running a grant the re-run would find held */
        if nil != granted && repository.GrantRoleGranted == outcome {
            fmt.Printf("granted role %q to user %q, but the listeners that drop the account's cache entries were not told: %v\n", role, user, grantErr)

            return fmt.Errorf("the role was granted, and the cache entries of user %q could not be dropped: %w", user, grantErr)
        }

        return grantErr
    }

    switch outcome {
    case repository.GrantRoleAccountAbsent:
        return fmt.Errorf("user %q disappeared before the role could be granted", user)
    case repository.GrantRoleAlreadyHeld:
        fmt.Printf("user %q already holds role %q; nothing to do\n", user, role)

        return nil
    }

    fmt.Printf("granted role %q to user %q\n", role, user)

    /* the listeners that drop the account's cache entries ran in THIS process: on the shared cache that is the server's view too, on the in-process fallback it is not, and a session opened against the server keeps the roles it cached until that server restarts */
    if true == cacheIsProcessLocal(runtimeInstance) {
        fmt.Println(processLocalCacheNotice)
    }

    return nil
}

var _ melodyclicontract.Command = (*GrantRoleCommand)(nil)
