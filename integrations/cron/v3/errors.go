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
    ErrK8sImageMissing                    = errors.New("cron: the k8s template requires a container image")
    ErrK8sInvalidName                     = errors.New("cron: command name does not yield a valid k8s resource name")
    ErrK8sDuplicateName                   = errors.New("cron: two commands map to the same k8s resource name")
    ErrK8sInvalidRestartPolicy            = errors.New("cron: k8s restartPolicy must be OnFailure or Never")
)
