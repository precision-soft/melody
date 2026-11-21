package lock

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

const exclusiveCommandLockNamePrefix = "melody:command:"

/* ExclusiveCommandConfig is what an exclusive command declares beyond the command and its locker; its zero value is the default. */
type ExclusiveCommandConfig struct {
    /* LockName is the lock every instance competes for: "" reads as "melody:command:" plus the command's name; name one when two differently-named commands must share one lock, or one command needs distinct locks per deployment. */
    LockName string

    /* Ttl is the lease handed to the locker, the crash safety and never the run cadence, as ExclusiveConfig.Ttl reads it: 30s by default, at least 2ms, unlimited selecting a session-style lock. */
    Ttl bound.Duration

    /* StopRenewalLimit is how long after the stop the lease is still renewed while the command winds down, as ExclusiveConfig.StopRenewalLimit reads it: 30s by default, raised to the refresh interval when that is longer; when set, at least 1ms and not below the refresh interval; unlimited renews until the command returns. A cron runner granting its commands a longer grace sets it to that grace. */
    StopRenewalLimit bound.Duration
}

var exclusiveCommandTtlRule = bound.Rule[time.Duration]{
    Name:              "ExclusiveCommandConfig.Ttl",
    Default:           exclusiveTtlRule.Default,
    Minimum:           exclusiveTtlRule.Minimum,
    UnlimitedAdmitted: true,
}

/* NewExclusiveCommand wraps a cli command so that, across every instance of the application, only one runs it at a time: the others skip quietly with a zero exit code. The configuration is resolved here, and a refusal panics naming the field. */
func NewExclusiveCommand(
    command clicontract.Command,
    locker lockcontract.Locker,
    config ExclusiveCommandConfig,
) *ExclusiveCommand {
    if true == internal.IsNilInterface(command) {
        exception.Panic(exception.NewError("exclusive command is nil", nil, nil))
    }

    if true == internal.IsNilInterface(locker) {
        exception.Panic(exception.NewError("exclusive command locker is nil", nil, nil))
    }

    ttl, ttlErr := bound.Resolve(config.Ttl, exclusiveCommandTtlRule)
    if nil != ttlErr {
        exception.Panic(exception.FromError(ttlErr))
    }

    if _, isLeaseLocker := locker.(lockcontract.LeaseLocker); true == isLeaseLocker && true == ttl.Unlimited {
        exception.Panic(
            exception.NewError(
                "an unlimited exclusive command ttl needs a session-style locker",
                exceptioncontract.Context{
                    "field": exclusiveCommandTtlRule.Name,
                },
                lockcontract.ErrUnlimitedTtlRefused,
            ),
        )
    }

    refreshInterval, _ := resolveRefreshSchedule(ttl)
    if _, stopRenewalLimitErr := resolveStopRenewalLimit("ExclusiveCommandConfig.StopRenewalLimit", config.StopRenewalLimit, refreshInterval); nil != stopRenewalLimitErr {
        exception.Panic(exception.FromError(stopRenewalLimitErr))
    }

    lockName := config.LockName
    if "" == lockName {
        lockName = exclusiveCommandLockNamePrefix + command.Name()
    }

    return &ExclusiveCommand{
        command:  command,
        locker:   locker,
        lockName: lockName,
        exclusiveConfig: ExclusiveConfig{
            Ttl:              ttl.Limit(),
            StopRenewalLimit: config.StopRenewalLimit,
        },
    }
}

/* ExclusiveCommand decorates a cli command with RunExclusive, the per-tick dedup for cron-launched commands on a multi-instance deployment. The ttl is crash-safety only, since the lease is refreshed while the command runs and released when it returns, so it is never tuned against the cron interval. */
type ExclusiveCommand struct {
    command         clicontract.Command
    locker          lockcontract.Locker
    lockName        string
    exclusiveConfig ExclusiveConfig
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
    runtimeInstance containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    ran, runErr := RunExclusive(
        runtimeInstance,
        instance.locker,
        instance.lockName,
        instance.exclusiveConfig,
        func(childRuntime containercontract.Runtime) error {
            return instance.command.Run(childRuntime, commandContext)
        },
    )
    if nil != runErr {
        return runErr
    }

    if false == ran {
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if false == internal.IsNilInterface(logger) {
            /* RunExclusive answers (false, nil) both for a lock held elsewhere and for a shutdown during the acquire, and this log line is the one record of "did not run", so the two get distinct messages */
            skipMessage := "command skipped: an exclusive run is already in progress on another instance"
            if nil != runtimeInstance.Context().Err() {
                skipMessage = "command skipped: shutdown was requested before the lock was acquired"
            }

            logger.Info(
                skipMessage,
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
