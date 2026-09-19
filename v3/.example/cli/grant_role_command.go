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

/* GrantRoleCommand grants an application role to an account.

   It declares its own --role flag to show that an application command may reuse a name the runtime also understands: the runtime's --mode/--role are recognized only before the command name, so `example:grant:role --role ROLE_ADMIN` reaches this command intact rather than being captured (and rejected) by the process-role parser. It also holds the user service through a container.Lazy handle built at command-registration time — the service is resolved at the first run, not when the command is constructed, so the boot-phase composition never resolves the container early.

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
    /* the flag is trimmed and judged against the roles the application knows BEFORE anything is read: the
       two admin doors normalise what they store, and this third role-writing door used to store the flag as
       typed — " ROLE_EDITOR" with its space, which the no-op check then never matched, so every re-run
       appended the role again and wrote an audit entry; and ROLE_ADMIM, which the voter compares exactly, so
       the console reported a grant that granted nothing */
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

    /* the role is ADDED through the repository's atomic door, which reads and writes the account under one
       lock: a grant that ran beside an admin update of the same account used to be a read, an append and a
       whole-set write, and whichever of the two wrote last took the other's change with it. The repository
       is the ONLY arbiter of whether the role is already held: the read above may have been served from a
       cache, and a short-cut on its roles answered "already holds" over an account the directory no longer
       showed holding it — the grant never reached the row. */
    granted, outcome, grantErr := userService.GrantRole(runtimeInstance, account.Id, role)
    if nil != grantErr {
        /* a grant whose write COMMITTED and whose listeners then refused is not a grant that failed: the
           role is in the directory, the cache entries of the account were not dropped, and the operator
           has to read both — a bare failure sent them to re-run a grant the re-run would find held */
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
