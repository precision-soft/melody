package cron

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const TemplateNameCrontab = "crontab"

/* TemplateNameCrontabNoUser renders the user-less crontab dialect: busybox crond (alpine images) and per-user `crontab` files reject the /etc/cron.d user column, so this variant omits it — no more cutting the column with sed in the image build. */
const TemplateNameCrontabNoUser = "crontab-no-user"

/* CrontabOwnershipMarker is the line every destination this generator's builtin templates render carries — the two crontab dialects in their header block, the k8s dialect as a leading YAML comment — so a later --prune can tell a file this generator wrote from one the operator put in the same directory. It names the command rather than saying "generated", because "GENERATED FILE" is what every generator writes and proves nothing about which one. The marker is shared across the builtin dialects: it proves the writing command, not the dialect, exactly as the two crontab variants already share it on the published majors. */
const CrontabOwnershipMarker = "# owned by melody:cron:generate"

const crontabHeaderBlock = `#############################################################################
#
# GENERATED FILE
# DO NOT EDIT LOCALLY
#
` + CrontabOwnershipMarker + `
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

const crontabNoUserHeaderBlock = `#############################################################################
#
# GENERATED FILE
# DO NOT EDIT LOCALLY
#
` + CrontabOwnershipMarker + `
#############################################################################
# Example of job definition (user-less dialect: busybox crond, per-user crontab):
# .---------------- minute (0 - 59)
# |  .------------- hour (0 - 23)
# |  |  .---------- day of month (1 - 31)
# |  |  |  .------- month (1 - 12) OR jan,feb,mar,apr ...
# |  |  |  |  .---- day of week (0 - 6) (Sunday=0 or 7) OR sun,mon,tue,wed,thu,fri,sat
# |  |  |  |  |
# *  *  *  *  * command to be executed
#############################################################################
`

const crontabFooterBlock = `#############################################################################
`

type CrontabTemplate struct {
    name              string
    includeUserColumn bool
}

var defaultCrontabTemplate = &CrontabTemplate{
    name:              TemplateNameCrontab,
    includeUserColumn: true,
}

var defaultCrontabNoUserTemplate = &CrontabTemplate{
    name:              TemplateNameCrontabNoUser,
    includeUserColumn: false,
}

func (instance *CrontabTemplate) Name() string {
    return instance.name
}

/* OwnershipMarker names the line both dialects carry in their header block, so --prune can prove a destination is one this template wrote before it empties it */
func (instance *CrontabTemplate) OwnershipMarker() string {
    return CrontabOwnershipMarker
}

/* RendersUserColumn answers for the generator's heartbeat-user guard before anything is rendered: the /etc/cron.d dialect places a user column on every line, the user-less dialect never does. */
func (instance *CrontabTemplate) RendersUserColumn() bool {
    return instance.includeUserColumn
}

func (instance *CrontabTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    var builder strings.Builder

    if true == instance.includeUserColumn {
        builder.WriteString(crontabHeaderBlock)
    } else {
        builder.WriteString(crontabNoUserHeaderBlock)
    }

    sectionsWritten := 0

    for _, entry := range entries {
        line, lineErr := buildCrontabLine(entry, instance.includeUserColumn)
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
        userColumn, userColumnErr := instance.heartbeatUserColumn(options, "cron: heartbeat command requires a non-empty heartbeat user", nil)
        if nil != userColumnErr {
            return "", userColumnErr
        }

        if validationErr := ValidateNoForbiddenCharacters(options.HeartbeatCommand, CrontabForbiddenCharacters, "heartbeat command"); nil != validationErr {
            return "", validationErr
        }

        if 0 < sectionsWritten {
            builder.WriteString("\n")
        }

        if true == instance.includeUserColumn {
            builder.WriteString(fmt.Sprintf("* * * * * %s %s\n", userColumn, joinShellTokens(options.HeartbeatCommand)))
        } else {
            builder.WriteString(fmt.Sprintf("* * * * * %s\n", joinShellTokens(options.HeartbeatCommand)))
        }

        sectionsWritten++
    } else if "" != options.HeartbeatPath {
        userColumn, userColumnErr := instance.heartbeatUserColumn(
            options,
            fmt.Sprintf("cron: heartbeat path %q requires a non-empty heartbeat user", options.HeartbeatPath),
            exceptioncontract.Context{"heartbeatPath": options.HeartbeatPath},
        )
        if nil != userColumnErr {
            return "", userColumnErr
        }

        if validationErr := ValidateNoForbiddenCharacters([]string{options.HeartbeatPath}, CrontabForbiddenCharacters, "heartbeat path"); nil != validationErr {
            return "", validationErr
        }

        if 0 < sectionsWritten {
            builder.WriteString("\n")
        }

        if true == instance.includeUserColumn {
            builder.WriteString(fmt.Sprintf("* * * * * %s /bin/touch %s\n", userColumn, shellQuoteIfNeeded(options.HeartbeatPath)))
        } else {
            builder.WriteString(fmt.Sprintf("* * * * * /bin/touch %s\n", shellQuoteIfNeeded(options.HeartbeatPath)))
        }

        sectionsWritten++
    }

    builder.WriteString(crontabFooterBlock)

    return builder.String(), nil
}

/* heartbeatUserColumn resolves the heartbeat line's user: the /etc/cron.d dialect requires and validates it, the user-less dialect ignores it entirely. */
func (instance *CrontabTemplate) heartbeatUserColumn(
    options RenderOptions,
    missingUserMessage string,
    missingUserContext exceptioncontract.Context,
) (string, error) {
    if false == instance.includeUserColumn {
        return "", nil
    }

    if "" == options.HeartbeatUser {
        return "", exception.NewError(missingUserMessage, missingUserContext, ErrHeartbeatUserMissing)
    }

    if userValidationErr := validateUserField("heartbeat user", options.HeartbeatUser); nil != userValidationErr {
        return "", userValidationErr
    }

    return options.HeartbeatUser, nil
}

func buildCrontabLine(entry Entry, includeUserColumn bool) (string, error) {
    if true == includeUserColumn {
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
    }

    if scheduleValidationErr := validateScheduleFields(entry, CrontabForbiddenCharacters, RunnerDialectCrontab); nil != scheduleValidationErr {
        return "", scheduleValidationErr
    }

    /* the user-less dialect targets busybox crond, which classifies a day field by its expanded values where vixie reads the spelling's first character — so a day-field pair the two daemons read differently is refused at generation, the same way the stepped single value is: emitting it would run one schedule in-process and another on the box. */
    if false == includeUserColumn && nil != entry.Schedule {
        dayOfMonthExpression := fieldOrWildcard(entry.Schedule.DayOfMonth)
        dayOfWeekExpression := normalizeCronNameTokens(fieldOrWildcard(entry.Schedule.DayOfWeek), cronDayOfWeekNameValues)

        if true == busyboxDayFieldsDiverge(dayOfMonthExpression, dayOfWeekExpression) {
            return "", exception.NewError(
                fmt.Sprintf(
                    "cron: entry %q pairs day fields (DayOfMonth %q, DayOfWeek %q) that busybox crond — the scheduler the user-less crontab dialect targets — runs as a different schedule than vixie crond and the in-process runner: busybox classifies a day field by its expanded values, so a field admitting every value is unused and the other governs alone, while vixie reads the first character; restrict a single day field and leave the other as the plain wildcard so every target reads the same schedule",
                    entry.Name,
                    dayOfMonthExpression,
                    dayOfWeekExpression,
                ),
                exceptioncontract.Context{
                    "entry":      entry.Name,
                    "dayOfMonth": dayOfMonthExpression,
                    "dayOfWeek":  dayOfWeekExpression,
                },
                ErrBusyboxDivergentDaySchedule,
            )
        }
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

        if validationErr := ValidateNoForbiddenCharacters(entry.Command, CrontabForbiddenCharacters, fmt.Sprintf("entry %q", entry.Name)); nil != validationErr {
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
        if validationErr := ValidateNoForbiddenCharacters(tokens, CrontabForbiddenCharacters, fmt.Sprintf("entry %q", entry.Name)); nil != validationErr {
            return "", validationErr
        }

        commandPart = joinShellTokens(tokens)
    }

    logRedirect := ""
    if "" != entry.LogPath {
        if validationErr := ValidateNoForbiddenCharacters([]string{entry.LogPath}, CrontabForbiddenCharacters, fmt.Sprintf("entry %q log path", entry.Name)); nil != validationErr {
            return "", validationErr
        }

        logRedirect = " >> " + singleQuote(entry.LogPath) + " 2>&1"
    }

    if false == includeUserColumn {
        return fmt.Sprintf(
            "%s %s%s",
            entry.Schedule.Expression(),
            commandPart,
            logRedirect,
        ), nil
    }

    return fmt.Sprintf(
        "%s %s %s%s",
        entry.Schedule.Expression(),
        entry.User,
        commandPart,
        logRedirect,
    ), nil
}

var (
    _ Template           = (*CrontabTemplate)(nil)
    _ OwnedTemplate      = (*CrontabTemplate)(nil)
    _ UserColumnTemplate = (*CrontabTemplate)(nil)
)
