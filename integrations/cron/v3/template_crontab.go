package cron

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const TemplateNameCrontab = "crontab"

/* TemplateNameCrontabNoUser renders the user-less crontab dialect for busybox crond and per-user crontab files, which reject the /etc/cron.d user column. */
const TemplateNameCrontabNoUser = "crontab-no-user"

/* CrontabOwnershipMarker opens the ownership line every builtin template renders, in the crontab header block or as a leading YAML comment, so --prune can tell a file this generator wrote from an operator's. The line a run writes is this prefix, " for " and the application's cli name, so it proves the writing application and command but not the dialect. Only a template handed the application's name renders the whole line; the builtin singletons and the package-level Render write the bare prefix, which no named application's sweep matches. */
const CrontabOwnershipMarker = "# owned by melody:cron:generate"

/* ownershipMarkerLine answers the exact line a template owned by the named application renders: the shared prefix, " for " and the name. An empty name keeps the bare prefix, which belongs to no application's sweep; the separator is a word so the line never coincides with a custom marker suffixed in brackets. */
func ownershipMarkerLine(applicationName string) string {
    if "" == applicationName {
        return CrontabOwnershipMarker
    }

    return CrontabOwnershipMarker + " for " + applicationName
}

const crontabHeaderBlockOpening = `#############################################################################
#
# GENERATED FILE
# DO NOT EDIT LOCALLY
#
`

/* crontabHeaderBlock is the /etc/cron.d dialect's header around the ownership line the template answers, so the line the file carries is the line the sweep asks for */
func crontabHeaderBlock(marker string) string {
    return crontabHeaderBlockOpening + marker + "\n" + crontabHeaderBlockLegend
}

const crontabHeaderBlockLegend = `#############################################################################
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

/* crontabNoUserHeaderBlock is the user-less dialect's header around the same ownership line */
func crontabNoUserHeaderBlock(marker string) string {
    return crontabHeaderBlockOpening + marker + "\n" + crontabNoUserHeaderBlockLegend
}

const crontabNoUserHeaderBlockLegend = `#############################################################################
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

    /* the application whose ownership line this template renders and answers; empty on the builtin singletons, set on the copy the generator derives for a run through ownedBy */
    applicationName string
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

/* OwnershipMarker names the line both dialects carry in their header block, so --prune can prove a destination is one this template wrote for this application before it empties it; on a template no application owns it is the bare prefix, which no sweep matches */
func (instance *CrontabTemplate) OwnershipMarker() string {
    return ownershipMarkerLine(instance.applicationName)
}

/* ownedBy answers a copy of this template that renders and answers the named application's ownership line, leaving the shared singleton unowned, so two applications sharing an output directory write different lines and each sweep recognises only its own. */
func (instance *CrontabTemplate) ownedBy(applicationName string) Template {
    owned := *instance
    owned.applicationName = applicationName

    return &owned
}

/* RendersUserColumn answers for the generator's heartbeat-user guard before anything is rendered: the /etc/cron.d dialect places a user column on every line, the user-less dialect never does. */
func (instance *CrontabTemplate) RendersUserColumn() bool {
    return instance.includeUserColumn
}

func (instance *CrontabTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    var builder strings.Builder

    if true == instance.includeUserColumn {
        builder.WriteString(crontabHeaderBlock(instance.OwnershipMarker()))
    } else {
        builder.WriteString(crontabNoUserHeaderBlock(instance.OwnershipMarker()))
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
            builder.WriteString(fmt.Sprintf("* * * * * %s %s\n", userColumn, JoinShellTokens(options.HeartbeatCommand)))
        } else {
            builder.WriteString(fmt.Sprintf("* * * * * %s\n", JoinShellTokens(options.HeartbeatCommand)))
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
            builder.WriteString(fmt.Sprintf("* * * * * %s /bin/touch %s\n", userColumn, ShellQuoteIfNeeded(options.HeartbeatPath)))
        } else {
            builder.WriteString(fmt.Sprintf("* * * * * /bin/touch %s\n", ShellQuoteIfNeeded(options.HeartbeatPath)))
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

    if userValidationErr := ValidateUserField("heartbeat user", options.HeartbeatUser); nil != userValidationErr {
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

        if userValidationErr := ValidateUserField(fmt.Sprintf("entry %q user", entry.Name), entry.User); nil != userValidationErr {
            return "", userValidationErr
        }
    }

    if scheduleValidationErr := ValidateScheduleFields(entry, CrontabForbiddenCharacters, RunnerDialectCrontab); nil != scheduleValidationErr {
        return "", scheduleValidationErr
    }

    /* busybox crond classifies a day field by its expanded values where vixie reads the spelling's first character, so a day-field pair the two read differently is refused at generation rather than run one schedule in-process and another on the box */
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

        commandPart = JoinShellTokens(entry.Command)
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

        commandPart = JoinShellTokens(tokens)
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
    _ Template                 = (*CrontabTemplate)(nil)
    _ OwnedTemplate            = (*CrontabTemplate)(nil)
    _ UserColumnTemplate       = (*CrontabTemplate)(nil)
    _ applicationOwnedTemplate = (*CrontabTemplate)(nil)
)
