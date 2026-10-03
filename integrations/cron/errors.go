package cron

import (
    "errors"
)

var (
    ErrNoOutputPath                       = errors.New("cron: no output path configured")
    ErrNoLogsDir                          = errors.New("cron: no logs directory configured")
    ErrTemplateNotFound                   = errors.New("cron: template not registered")
    ErrHeartbeatUserMissing               = errors.New("cron: heartbeat is configured but no user is set")
    ErrHeartbeatDestinationUnmatched      = errors.New("cron: heartbeat destination does not match any written destination")
    ErrHeartbeatDestinationDefaultMissing = errors.New("cron: heartbeat destination 'default' requested but the default destination has no entries")
    ErrDestinationEscape                  = errors.New("cron: path escapes the allowed directory")
    ErrEntryEmptyUser                     = errors.New("cron: entry has empty user")
    ErrEntryEmptyCommand                  = errors.New("cron: entry has no command to run")
    ErrForbiddenCharacter                 = errors.New("cron: token contains forbidden character")
    ErrFieldContainsWhitespace            = errors.New("cron: field contains whitespace")
    ErrSteppedSingleValue                 = errors.New("cron: field steps a single value, which the target schedulers read differently")
    ErrBusyboxDivergentDaySchedule        = errors.New("cron: day fields mean a different schedule under busybox crond")
    ErrInvalidSchedule                    = errors.New("cron: schedule field is not a valid cron expression")
    ErrUnknownScheduledCommand            = errors.New("cron: scheduled command has no matching registered command")
    ErrUnsupportedRunnerEntry             = errors.New("cron: the in-process runner supports only name-scheduled single-instance entries")
    ErrUnknownRunnerDialect               = errors.New("cron: unknown runner dialect")
    ErrDuplicateRunnerCommand             = errors.New("cron: two runner commands share one name")
    ErrSharedRunnerCommandFlags           = errors.New("cron: runner command returns shared flag instances")
    ErrCommandTimeout                     = errors.New("cron: scheduled command exceeded its timeout")
)

/* commandTimeoutFailure is the single-valued link that lets one error answer both errors.Is against ErrCommandTimeout and errors.Is against the command's own sentinel. errors.Join is not used because its Unwrap is []error, while exception.LogContext follows errors.Unwrap, the single-value form, so a joined value would reach the record with no cause. */
type commandTimeoutFailure struct {
    cause error
}

func (instance *commandTimeoutFailure) Error() string {
    return ErrCommandTimeout.Error()
}

func (instance *commandTimeoutFailure) Is(target error) bool {
    return ErrCommandTimeout == target
}

func (instance *commandTimeoutFailure) Unwrap() error {
    return instance.cause
}

/* commandTimeoutCause answers the cause a timeout error wraps: the sentinel alone when the command reported nothing, and the sentinel over the command's own failure when it did, so both stay reachable down one chain. */
func commandTimeoutCause(runErr error) error {
    if nil == runErr || true == isNilInterface(runErr) {
        return ErrCommandTimeout
    }

    return &commandTimeoutFailure{cause: runErr}
}
