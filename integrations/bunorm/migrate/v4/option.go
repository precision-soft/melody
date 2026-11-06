package migrate

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

/* The defaults an Options field takes when left out. */
const (
    DefaultManagerRegistryServiceId = "service.database.manager.registry"
    DefaultManagerFlagName          = "manager"
    DefaultCommandPrefix            = "db"
)

type Options struct {
    ManagerRegistryServiceId string
    ManagerFlagName          string
    CommandPrefix            string

    /* ManagerName pins the registry manager these commands use when the --manager flag is absent; empty falls back to the registry default. Pin one manager per command set so a multi-context binary can never migrate the wrong database by omitting the flag. */
    ManagerName string

    /* UnlockTimeout bounds the release of the migration lock after a run, detached from the command's context so an interrupted migration still frees the lock row: 5s when left out, at least 1ms, unlimited refused, since the exit waits on it. */
    UnlockTimeout bound.Duration
}

var optionsUnlockTimeoutRule = bound.Rule[time.Duration]{
    Name:    "Options.UnlockTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

func mustResolveUnlockTimeout(options Options) time.Duration {
    unlockTimeout, unlockTimeoutErr := bound.Resolve(options.UnlockTimeout, optionsUnlockTimeoutRule)
    if nil != unlockTimeoutErr {
        exception.Panic(exception.FromError(unlockTimeoutErr))
    }

    return unlockTimeout.Value
}
