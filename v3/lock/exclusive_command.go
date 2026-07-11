package lock

import (
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    lockcontract "github.com/precision-soft/melody/v3/lock/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const exclusiveCommandLockNamePrefix = "melody:command:"

/* NewExclusiveCommand wraps a cli command so that, across every instance of the application, only one runs it at a time: the others skip quietly with a zero exit code. The lock name defaults to "melody:command:" plus the command name; use NewExclusiveCommandWithName when two differently-named commands must share one lock, or one command needs distinct locks per deployment. */
func NewExclusiveCommand(
    command clicontract.Command,
    locker lockcontract.Locker,
    ttl time.Duration,
) *ExclusiveCommand {
    if true == internal.IsNilInterface(command) {
        exception.Panic(exception.NewError("exclusive command is nil", nil, nil))
    }

    return NewExclusiveCommandWithName(command, locker, exclusiveCommandLockNamePrefix+command.Name(), ttl)
}

func NewExclusiveCommandWithName(
    command clicontract.Command,
    locker lockcontract.Locker,
    lockName string,
    ttl time.Duration,
) *ExclusiveCommand {
    if true == internal.IsNilInterface(command) {
        exception.Panic(exception.NewError("exclusive command is nil", nil, nil))
    }

    if true == internal.IsNilInterface(locker) {
        exception.Panic(exception.NewError("exclusive command locker is nil", nil, nil))
    }

    if "" == lockName {
        exception.Panic(exception.NewError("exclusive command lock name is empty", nil, nil))
    }

    return &ExclusiveCommand{
        command:  command,
        locker:   locker,
        lockName: lockName,
        ttl:      ttl,
    }
}

/* ExclusiveCommand decorates a cli command with RunExclusive, the per-tick dedup for cron-launched commands on a multi-instance deployment: the ttl is crash-safety only (the lease is refreshed while the command runs and released as soon as it returns), so it never has to be tuned against the cron interval or the command duration. */
type ExclusiveCommand struct {
    command  clicontract.Command
    locker   lockcontract.Locker
    lockName string
    ttl      time.Duration
}

func (instance *ExclusiveCommand) Name() string {
    return instance.command.Name()
}

func (instance *ExclusiveCommand) Description() string {
    return instance.command.Description()
}

func (instance *ExclusiveCommand) Flags() []clicontract.Flag {
    return instance.command.Flags()
}

func (instance *ExclusiveCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    ran, runErr := RunExclusive(
        runtimeInstance,
        instance.locker,
        instance.lockName,
        instance.ttl,
        func(childRuntime runtimecontract.Runtime) error {
            return instance.command.Run(childRuntime, commandContext)
        },
    )
    if nil != runErr {
        return runErr
    }

    if false == ran {
        /* another instance holds the lock for this tick; exit zero so cron stays green everywhere */
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Info(
                "command skipped: an exclusive run is already in progress on another instance",
                loggingcontract.Context{
                    "command": instance.command.Name(),
                    "lock":    instance.lockName,
                },
            )
        }
    }

    return nil
}

var _ clicontract.Command = (*ExclusiveCommand)(nil)
