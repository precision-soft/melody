package cron

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const TemplateNameCrontab = "crontab"

const crontabHeaderBlock = `#############################################################################
#
# GENERATED FILE
# DO NOT EDIT LOCALLY
#
#############################################################################
# Example of job definition:
# .---------------- minute (0 - 59)
# |  .------------- hour (0 - 23)
# |  |  .---------- day of month (1 - 31)
# |  |  |  .------- month (1 - 12) OR jan,feb,mar,apr ...
# |  |  |  |  .---- day of week (0 - 6) (Sunday=0 or 7) OR sun,mon,tue,wed,thu,fri,sat
# |  |  |  |  |
# *  *  *  *  * user-name command to be executed
#############################################################################
`

const crontabFooterBlock = `#############################################################################
`

type CrontabTemplate struct{}

var defaultCrontabTemplate = &CrontabTemplate{}

func (instance *CrontabTemplate) Name() string {
    return TemplateNameCrontab
}

func (instance *CrontabTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    var builder strings.Builder

    builder.WriteString(crontabHeaderBlock)

    sectionsWritten := 0

    for _, entry := range entries {
        line, lineErr := buildCrontabLine(entry)
        if nil != lineErr {
            return "", lineErr
        }

        if 0 < sectionsWritten {
            builder.WriteString("\n")
        }

        builder.WriteString(line)
        builder.WriteString("\n")

        sectionsWritten++
    }

    if 0 < len(options.HeartbeatCommand) {
        if "" == options.HeartbeatUser {
            return "", exception.NewError(
                "cron: heartbeat command requires a non-empty heartbeat user",
                nil,
                ErrHeartbeatUserMissing,
            )
        }

        if userValidationErr := validateUserField("heartbeat user", options.HeartbeatUser); nil != userValidationErr {
            return "", userValidationErr
        }

        if validationErr := ValidateNoForbiddenChars(options.HeartbeatCommand, CrontabForbiddenChars, "heartbeat command"); nil != validationErr {
            return "", validationErr
        }

        if 0 < sectionsWritten {
            builder.WriteString("\n")
        }

        builder.WriteString(fmt.Sprintf("* * * * * %s %s\n", options.HeartbeatUser, joinShellTokens(options.HeartbeatCommand)))

        sectionsWritten++
    } else if "" != options.HeartbeatPath {
        if "" == options.HeartbeatUser {
            return "", exception.NewError(
                fmt.Sprintf("cron: heartbeat path %q requires a non-empty heartbeat user", options.HeartbeatPath),
                exceptioncontract.Context{"heartbeatPath": options.HeartbeatPath},
                ErrHeartbeatUserMissing,
            )
        }

        if userValidationErr := validateUserField("heartbeat user", options.HeartbeatUser); nil != userValidationErr {
            return "", userValidationErr
        }

        if validationErr := ValidateNoForbiddenChars([]string{options.HeartbeatPath}, CrontabForbiddenChars, "heartbeat path"); nil != validationErr {
            return "", validationErr
        }

        if 0 < sectionsWritten {
            builder.WriteString("\n")
        }

        builder.WriteString(fmt.Sprintf("* * * * * %s /bin/touch %s\n", options.HeartbeatUser, shellQuoteIfNeeded(options.HeartbeatPath)))

        sectionsWritten++
    }

    builder.WriteString(crontabFooterBlock)

    return builder.String(), nil
}

func buildCrontabLine(entry Entry) (string, error) {
    if "" == entry.User {
        return "", exception.NewError(
            fmt.Sprintf("cron: command %q has no user; set EntryConfig.User on the schedule, pass --user, or register the melody.cron.user parameter", entry.Name),
            exceptioncontract.Context{"entry": entry.Name},
            ErrEntryEmptyUser,
        )
    }

    if userValidationErr := validateUserField(fmt.Sprintf("entry %q user", entry.Name), entry.User); nil != userValidationErr {
        return "", userValidationErr
    }

    if scheduleValidationErr := validateScheduleFields(entry); nil != scheduleValidationErr {
        return "", scheduleValidationErr
    }

    var commandPart string
    if 0 < len(entry.Command) {
        if "" == strings.Join(entry.Command, "") {
            return "", exception.NewError(
                fmt.Sprintf("cron: entry %q has Command but every token is empty; remove the override or supply a non-empty command", entry.Name),
                exceptioncontract.Context{"entry": entry.Name},
                ErrEntryEmptyCommand,
            )
        }

        if validationErr := ValidateNoForbiddenChars(entry.Command, CrontabForbiddenChars, fmt.Sprintf("entry %q", entry.Name)); nil != validationErr {
            return "", validationErr
        }

        commandPart = joinShellTokens(entry.Command)
    } else {
        if "" == entry.Binary {
            return "", exception.NewError(
                fmt.Sprintf("cron: entry %q has empty binary and no command override; nothing to schedule", entry.Name),
                exceptioncontract.Context{"entry": entry.Name},
                ErrEntryEmptyCommand,
            )
        }

        tokens := append([]string{entry.Binary}, entry.Args...)
        if validationErr := ValidateNoForbiddenChars(tokens, CrontabForbiddenChars, fmt.Sprintf("entry %q", entry.Name)); nil != validationErr {
            return "", validationErr
        }

        commandPart = joinShellTokens(tokens)
    }

    logRedirect := ""
    if "" != entry.LogPath {
        if validationErr := ValidateNoForbiddenChars([]string{entry.LogPath}, CrontabForbiddenChars, fmt.Sprintf("entry %q log path", entry.Name)); nil != validationErr {
            return "", validationErr
        }

        logRedirect = " >> " + singleQuote(entry.LogPath) + " 2>&1"
    }

    return fmt.Sprintf(
        "%s %s %s%s",
        entry.Schedule.Expression(),
        entry.User,
        commandPart,
        logRedirect,
    ), nil
}

var _ Template = (*CrontabTemplate)(nil)
