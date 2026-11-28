package output

import (
    "runtime"
    "time"

    melodyversion "github.com/precision-soft/melody/v4/version"
)

/* MetaConfig is what an envelope's meta is built from beside the command; a field left out is empty, and an empty Melody or Go version is the running binary's. */
type MetaConfig struct {
    Arguments []string
    Option    Options
    StartedAt time.Time
    Duration  time.Duration
    Version   Version
}

func NewMeta(command string, config MetaConfig) Meta {
    option := config.Option
    version := config.Version
    normalized := normalizeOptionChoices(option)

    if "" == version.Melody {
        version.Melody = melodyversion.BuildVersion()
    }

    /* the option is the one source of the application's version; a caller's Application is not read */
    version.Application = option.ApplicationVersion

    /* as for melody's: the caller's value wins when present, the running binary's otherwise */
    if "" == version.Go {
        version.Go = runtime.Version()
    }

    return Meta{
        Command:   command,
        Arguments: copyStringSlice(config.Arguments),
        Flags: Flags{
            Format:  normalized.Format,
            NoColor: normalized.NoColor,
            Verbose: normalized.Verbose,
            Quiet:   normalized.Quiet,
            Order:   normalized.Order,
            Limit:   normalized.Limit,
            Offset:  normalized.Offset,
        },
        StartedAt:            config.StartedAt,
        DurationMilliseconds: config.Duration.Milliseconds(),
        Version: Version{
            Application: version.Application,
            Melody:      version.Melody,
            Go:          version.Go,
        },
    }
}

func NewEnvelope(
    meta Meta,
) Envelope {
    return Envelope{
        Meta:     meta,
        Data:     nil,
        Table:    nil,
        Warnings: []Warning{},
        Error:    nil,
    }
}

func NewWarning(code string, message string, details map[string]any) Warning {
    return Warning{
        Code:    code,
        Message: message,
        Details: details,
    }
}

/* ErrorConfig is what an envelope error carries beside its code and message; a field left out is empty. */
type ErrorConfig struct {
    Details map[string]any
    Cause   *ErrorCause
}

func NewError(code string, message string, config ErrorConfig) *Error {
    return &Error{
        Code:    code,
        Message: message,
        Details: config.Details,
        Cause:   config.Cause,
    }
}

func NewErrorCause(message string, details map[string]any) *ErrorCause {
    return &ErrorCause{
        Message: message,
        Details: details,
    }
}

func copyStringSlice(value []string) []string {
    if nil == value {
        return []string{}
    }

    copied := make([]string, len(value))
    copy(copied, value)

    return copied
}
