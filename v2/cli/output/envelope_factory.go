package output

import (
	"runtime"
	"time"

	melodyversion "github.com/precision-soft/melody/v2/version"
)

func NewMeta(
	command string,
	arguments []string,
	option Option,
	startedAt time.Time,
	duration time.Duration,
	version Version,
) Meta {
	normalized := NormalizeOption(option)

	if "" == version.Melody {
		version.Melody = melodyversion.BuildVersion()
	}

	if "" == version.Application {
		version.Application = getApplicationVersion()
	}

	return Meta{
		Command:   command,
		Arguments: copyStringSlice(arguments),
		Flags: Flags{
			Format:  normalized.Format,
			NoColor: normalized.NoColor,
			Verbose: normalized.Verbose,
			Quiet:   normalized.Quiet,
			Fields:  copyStringSlice(normalized.Fields),
			SortKey: normalized.SortKey,
			Order:   normalized.Order,
			Limit:   normalized.Limit,
			Offset:  normalized.Offset,
		},
		StartedAt:            startedAt,
		DurationMilliseconds: duration.Milliseconds(),
		Version: Version{
			Application: version.Application,
			Melody:      version.Melody,
			Go:          runtime.Version(),
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

func NewError(code string, message string, details map[string]any, cause *ErrorCause) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Details: details,
		Cause:   cause,
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
