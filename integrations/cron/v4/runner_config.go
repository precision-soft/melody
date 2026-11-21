package cron

import (
    "errors"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* RunnerConfig declares the in-process runner. Commands are the registered commands the configuration schedules by name, an entry naming a command absent from the list refused at construction. Dialect is the day-of-month / day-of-week rule, crontab when left out, any other value than the two constants refused. Timeout and GracefulTimeout are what an entry that names none of its own runs under. */
type RunnerConfig struct {
    /* Dialect selects the day-of-month / day-of-week rule. The zero value and RunnerDialectCrontab follow vixie crond, where a star-based day field, plain or stepped, is unrestricted and the day fields combine with and; RunnerDialectKubernetes follows the robfig scheduler behind the k8s template, where only the plain or unit-stepped wildcard, alone or in a list, is unrestricted and a wildcard stepped above one combines with or. Two restricted day fields combine with or in both. */
    Dialect RunnerDialect

    /* Commands are the registered commands the configuration schedules by name; wrap a command in lock.NewExclusiveCommand for multi-instance safety before listing it. */
    Commands []clicontract.Command

    /* OutputCaptureLimit is what one run may write into the journal, so a job printing a row per record cannot write its whole result set into one log line: 64 KiB by default, at least 1, unlimited refused. */
    OutputCaptureLimit bound.Count

    /* ClockJumpResetThreshold is the vixie-cron bound on wall-clock reconciliation: a jump smaller than this is resolved minute by minute, one of at least this much in either direction re-anchors the minute chain without catch-up. 3h by default, at least 2m, since at one minute every ordinary tick would re-anchor, in whole minutes, the unit the algorithm counts in, and unlimited refused, since a chain that never re-anchors catches up every minute of any jump. */
    ClockJumpResetThreshold bound.Duration

    /* Timeout bounds one run of an entry that names no timeout of its own: 1h by default, at least 1ms, unlimited admitted for runs no deadline should end. A deadline cancels the command's context and is not a kill. */
    Timeout bound.Duration

    /* GracefulTimeout is how long the command of an entry that names no window of its own is given to unwind after its context was cancelled, by its deadline or by the shutdown, before the runner stops waiting and closes the run's container scope under it: 5m by default, at least 1ms, unlimited refused, since an abandon that never comes is the leak the window ends. */
    GracefulTimeout bound.Duration
}

var runnerOutputCaptureLimitRule = bound.Rule[int]{
    Name:    "RunnerConfig.OutputCaptureLimit",
    Default: 64 * 1024,
    Minimum: 1,
}

var runnerClockJumpResetThresholdRule = bound.Rule[time.Duration]{
    Name:    "RunnerConfig.ClockJumpResetThreshold",
    Default: 3 * time.Hour,
    Minimum: 2 * time.Minute,
}

var runnerTimeoutRule = bound.Rule[time.Duration]{
    Name:              "RunnerConfig.Timeout",
    Default:           time.Hour,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

var runnerGracefulTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RunnerConfig.GracefulTimeout",
    Default: 5 * time.Minute,
    Minimum: bound.TimeoutMinimum,
}

var entryTimeoutRule = bound.Rule[time.Duration]{
    Name:              "EntryConfig.Timeout",
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

var entryGracefulTimeoutRule = bound.Rule[time.Duration]{
    Name:    "EntryConfig.GracefulTimeout",
    Minimum: bound.TimeoutMinimum,
}

var entryInstancesRule = bound.Rule[int]{
    Name:    "EntryConfig.Instances",
    Default: 1,
    Minimum: 1,
}

type resolvedRunner struct {
    dialect                 RunnerDialect
    outputCaptureLimit      int
    clockJumpResetThreshold time.Duration
    timeout                 bound.Resolved[time.Duration]
    gracefulTimeout         time.Duration
}

func resolveRunnerConfig(config RunnerConfig) (resolvedRunner, error) {
    if _, dialectErr := resolveRunnerDialect(config.Dialect); nil != dialectErr {
        return resolvedRunner{}, dialectErr
    }

    outputCaptureLimit, outputCaptureLimitErr := bound.Resolve(config.OutputCaptureLimit, runnerOutputCaptureLimitRule)
    if nil != outputCaptureLimitErr {
        return resolvedRunner{}, outputCaptureLimitErr
    }

    clockJumpResetThreshold, clockJumpResetThresholdErr := bound.Resolve(config.ClockJumpResetThreshold, runnerClockJumpResetThresholdRule)
    if nil != clockJumpResetThresholdErr {
        return resolvedRunner{}, clockJumpResetThresholdErr
    }

    if 0 != clockJumpResetThreshold.Value%time.Minute {
        return resolvedRunner{}, exception.NewError(
            "the clock jump reset threshold is not a whole number of minutes",
            exceptioncontract.Context{
                "field": runnerClockJumpResetThresholdRule.Name,
                "value": clockJumpResetThreshold.Value.String(),
            },
            nil,
        )
    }

    timeout, timeoutErr := bound.Resolve(config.Timeout, runnerTimeoutRule)
    if nil != timeoutErr {
        return resolvedRunner{}, timeoutErr
    }

    gracefulTimeout, gracefulTimeoutErr := bound.Resolve(config.GracefulTimeout, runnerGracefulTimeoutRule)
    if nil != gracefulTimeoutErr {
        return resolvedRunner{}, gracefulTimeoutErr
    }

    return resolvedRunner{
        dialect:                 config.Dialect,
        outputCaptureLimit:      outputCaptureLimit.Value,
        clockJumpResetThreshold: clockJumpResetThreshold.Value,
        timeout:                 timeout,
        gracefulTimeout:         gracefulTimeout.Value,
    }, nil
}

/* resolveEntryTimeout resolves the deadline one entry runs under: the runner's when the entry names none, its own by the entry rule otherwise; zero, the value an unlimited limit resolves to, means no deadline. */
func resolveEntryTimeout(commandName string, timeout bound.Duration, runner resolvedRunner) (time.Duration, error) {
    resolved := runner.timeout

    if false == timeout.IsAbsent() {
        entryTimeout, entryTimeoutErr := bound.Resolve(timeout, entryTimeoutRule)
        if nil != entryTimeoutErr {
            return 0, withCommandName(entryTimeoutErr, commandName)
        }

        resolved = entryTimeout
    }

    return resolved.Value, nil
}

/* resolveEntryGracefulTimeout resolves the unwind window an entry names for itself, zero when it names none, so the runner's window governs it where the window is used. */
func resolveEntryGracefulTimeout(commandName string, gracefulTimeout bound.Duration) (time.Duration, error) {
    if true == gracefulTimeout.IsAbsent() {
        return 0, nil
    }

    resolved, resolveErr := bound.Resolve(gracefulTimeout, entryGracefulTimeoutRule)
    if nil != resolveErr {
        return 0, withCommandName(resolveErr, commandName)
    }

    return resolved.Value, nil
}

/* resolveEntryInstances resolves how many parallel runs the generated manifests expand an entry into. */
func resolveEntryInstances(commandName string, instances bound.Count) (int, error) {
    resolved, resolveErr := bound.Resolve(instances, entryInstancesRule)
    if nil != resolveErr {
        return 0, withCommandName(resolveErr, commandName)
    }

    return resolved.Value, nil
}

func withCommandName(err error, commandName string) error {
    var exceptionErr *exception.Error
    if true == errors.As(err, &exceptionErr) {
        exceptionErr.SetContextValue("commandName", commandName)
    }

    return err
}
