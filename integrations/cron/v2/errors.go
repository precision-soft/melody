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
    ErrInvalidSchedule                    = errors.New("cron: schedule field is not a valid cron expression")
    ErrUnknownScheduledCommand            = errors.New("cron: scheduled command has no matching registered command")
    ErrUnsupportedRunnerEntry             = errors.New("cron: the in-process runner supports only name-scheduled single-instance entries")
    ErrUnknownRunnerDialect               = errors.New("cron: unknown runner dialect")
    ErrDuplicateRunnerCommand             = errors.New("cron: two runner commands share one name")
    ErrSharedRunnerCommandFlags           = errors.New("cron: runner command returns shared flag instances")
)
