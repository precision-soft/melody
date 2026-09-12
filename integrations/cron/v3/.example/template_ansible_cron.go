package example

import (
    "errors"
    "fmt"
    "strings"
    "unicode/utf8"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* ansibleCronOwnershipMarker is this template's own marker, distinct from the builtin one: --prune empties only files whose first bytes carry the marker of the template generating now, so a dialect of the integrator's own declares a line of its own. */
const ansibleCronOwnershipMarker = "# owned by melody:cron:generate (ansible-cron)"

/* ansibleCronHeartbeatName is the cron name of the heartbeat task, the identity ansible.builtin.cron keeps for the crontab line it writes for it. */
const ansibleCronHeartbeatName = "melody heartbeat"

/* ErrAnsibleCronDuplicateName is returned when two entries of one destination render the same cron name: ansible.builtin.cron keeps ONE crontab line per name, so the second task would silently replace the first. */
var ErrAnsibleCronDuplicateName = errors.New("ansible-cron: two entries render the same cron name")

/* ErrAnsibleCronInvalidUtf8 is returned for a value that is not valid UTF-8: the YAML scalar escapes such a byte as \xNN, which a YAML reader decodes as the code point U+00NN — a different byte sequence than the one configured, handed to the shell in silence. */
var ErrAnsibleCronInvalidUtf8 = errors.New("ansible-cron: value is not valid UTF-8")

/* the cron name becomes the "#Ansible: <name>" comment line the module writes above the crontab line it manages; a line terminator inside it ends the comment and starts a crontab line of the name's own choosing. Nothing else is refused in it: % is inert in a crontab comment, and the name never reaches a shell. */
var ansibleCronNameForbiddenCharacters = []melodycron.ForbiddenCharacter{
    {Char: '\n', Reason: "a literal newline ends the #Ansible: comment line and starts a crontab line of its own; remove it at the source"},
    {Char: '\r', Reason: "a carriage return ends the #Ansible: comment line on many cron daemons; remove it at the source"},
}

/* AnsibleCronTemplate renders every entry as one ansible.builtin.cron task, so the same registry that drives the in-process runner can also feed a playbook. The five schedule fields map one to one onto the module's minute/hour/day/month/weekday arguments, which is what keeps this dialect a faithful rendering rather than an expression conversion — and what makes it a genuinely userland dialect: the binding ships no ansible template of its own.

   The dialect lives under crontab semantics, because that is where the module puts the values: ansible.builtin.cron writes the crontab line itself as the seven arguments joined on a space — minute, hour, day, month, weekday, user, job — with no validation of the schedule fields, no escaping of the job (a % is the crontab line continuation there as anywhere else; the module's documentation asks the caller to escape it), and the name as the "#Ansible: <name>" comment line by which it finds the entry again. So the template holds the schedule fields, the user and the job to exactly what the builtin crontab dialect holds them to, through the binding's exported validators, and renders the job through the binding's shell quoting: a field carrying a space would otherwise render a second crontab line, an argument carrying a space would arrive at the process as two, and a % would end the command where it stands.

   TaskNamePrefix is the template's own configuration, injected at construction the way every custom template carries its knobs. It reaches only the play's task name, which ansible prints and never writes anywhere, so it is not validated. */
type AnsibleCronTemplate struct {
    TaskNamePrefix string
}

func (instance *AnsibleCronTemplate) Name() string {
    return "ansible-cron"
}

/* OwnershipMarker opts this dialect into --prune: a playbook file this generator wrote earlier and no longer produces is emptied down to the marker comment instead of running its retired tasks forever. */
func (instance *AnsibleCronTemplate) OwnershipMarker() string {
    return ansibleCronOwnershipMarker
}

/* RendersUserColumn answers true because every task carries the cron module's user argument, so a heartbeat line rendered here needs the user exactly as the /etc/cron.d dialect does. */
func (instance *AnsibleCronTemplate) RendersUserColumn() bool {
    return true
}

func (instance *AnsibleCronTemplate) Render(entries []melodycron.Entry, options melodycron.RenderOptions) (string, error) {
    var builder strings.Builder
    builder.WriteString(ansibleCronOwnershipMarker + "\n---\n")

    /* ansible.builtin.cron keeps one crontab line per name, and find_job answers the first "#Ansible: <name>" comment it meets — so two tasks sharing a name are one line, the last one written, and the entry that lost is gone in silence */
    namesSeen := make(map[string]string, len(entries))

    for _, entry := range entries {
        cronName, task, taskErr := instance.buildTask(entry)
        if nil != taskErr {
            return "", taskErr
        }

        if existing, seen := namesSeen[cronName]; true == seen {
            return "", exception.NewError(
                fmt.Sprintf("ansible-cron: entries %q and %q both render the cron name %q, which ansible.builtin.cron uses as the identity of the crontab line it manages; rename one so each entry keeps a line of its own", existing, entry.Name, cronName),
                exceptioncontract.Context{
                    "name":          cronName,
                    "entry":         entry.Name,
                    "conflictsWith": existing,
                },
                ErrAnsibleCronDuplicateName,
            )
        }

        namesSeen[cronName] = entry.Name

        builder.WriteString(task)
    }

    heartbeat, heartbeatErr := instance.buildHeartbeatTask(options)
    if nil != heartbeatErr {
        return "", heartbeatErr
    }

    builder.WriteString(heartbeat)

    return builder.String(), nil
}

/* buildTask renders one entry as one task and returns the cron name it rendered under, the identity ansible.builtin.cron keeps for the line. A command expanded into several parallel instances yields one entry per instance under the same command name, so the cron name carries the instance when there is more than one — the way the k8s dialect suffixes its resource name — or every instance past the first would replace the one before it. */
func (instance *AnsibleCronTemplate) buildTask(entry melodycron.Entry) (string, string, error) {
    if "" == entry.User {
        return "", "", exception.NewError(
            fmt.Sprintf("ansible-cron: command %q has no user; set EntryConfig.User on the schedule, pass --user, or register the melody.cron.user parameter", entry.Name),
            exceptioncontract.Context{"entry": entry.Name},
            melodycron.ErrEntryEmptyUser,
        )
    }

    if userValidationErr := melodycron.ValidateUserField(fmt.Sprintf("ansible-cron entry %q user", entry.Name), entry.User); nil != userValidationErr {
        return "", "", userValidationErr
    }

    if scheduleValidationErr := melodycron.ValidateScheduleFields(entry, melodycron.CrontabForbiddenCharacters, melodycron.RunnerDialectCrontab); nil != scheduleValidationErr {
        return "", "", scheduleValidationErr
    }

    tokens, tokensErr := ansibleCronJobTokens(entry)
    if nil != tokensErr {
        return "", "", tokensErr
    }

    if validationErr := melodycron.ValidateNoForbiddenCharacters(tokens, melodycron.CrontabForbiddenCharacters, fmt.Sprintf("ansible-cron entry %q", entry.Name)); nil != validationErr {
        return "", "", validationErr
    }

    if utf8Err := refuseInvalidUtf8(entry.Name, append([]string{entry.User}, tokens...)); nil != utf8Err {
        return "", "", utf8Err
    }

    cronName := entry.Name
    if 1 < entry.InstanceCount {
        cronName = fmt.Sprintf("%s (%d/%d)", entry.Name, entry.InstanceIndex, entry.InstanceCount)
    }

    if validationErr := melodycron.ValidateNoForbiddenCharacters([]string{cronName}, ansibleCronNameForbiddenCharacters, fmt.Sprintf("ansible-cron entry %q name", entry.Name)); nil != validationErr {
        return "", "", validationErr
    }

    schedule := entry.Schedule
    if nil == schedule {
        schedule = &melodycron.Schedule{}
    }

    task := fmt.Sprintf(
        "- name: %s\n  ansible.builtin.cron:\n    name: %s\n    minute: %s\n    hour: %s\n    day: %s\n    month: %s\n    weekday: %s\n    user: %s\n    job: %s\n",
        yamlScalar(instance.TaskNamePrefix+cronName),
        yamlScalar(cronName),
        yamlScalar(fieldOrEveryValue(schedule.Minute)),
        yamlScalar(fieldOrEveryValue(schedule.Hour)),
        yamlScalar(fieldOrEveryValue(schedule.DayOfMonth)),
        yamlScalar(fieldOrEveryValue(schedule.Month)),
        yamlScalar(fieldOrEveryValue(schedule.DayOfWeek)),
        yamlScalar(entry.User),
        yamlScalar(melodycron.JoinShellTokens(tokens)),
    )

    return cronName, task, nil
}

/* buildHeartbeatTask renders the heartbeat the way the /etc/cron.d dialect does: a heartbeat command wins over a heartbeat path, either needs the heartbeat user, and both go through the same shell quoting as an entry. It renders nothing when neither is configured. */
func (instance *AnsibleCronTemplate) buildHeartbeatTask(options melodycron.RenderOptions) (string, error) {
    var job string

    if 0 < len(options.HeartbeatCommand) {
        if validationErr := melodycron.ValidateNoForbiddenCharacters(options.HeartbeatCommand, melodycron.CrontabForbiddenCharacters, "ansible-cron heartbeat command"); nil != validationErr {
            return "", validationErr
        }

        job = melodycron.JoinShellTokens(options.HeartbeatCommand)
    } else if "" != options.HeartbeatPath {
        if validationErr := melodycron.ValidateNoForbiddenCharacters([]string{options.HeartbeatPath}, melodycron.CrontabForbiddenCharacters, "ansible-cron heartbeat path"); nil != validationErr {
            return "", validationErr
        }

        job = "/bin/touch " + melodycron.ShellQuoteIfNeeded(options.HeartbeatPath)
    } else {
        return "", nil
    }

    if "" == options.HeartbeatUser {
        return "", exception.NewError(
            "ansible-cron: the heartbeat requires a non-empty heartbeat user",
            exceptioncontract.Context{"heartbeatPath": options.HeartbeatPath},
            melodycron.ErrHeartbeatUserMissing,
        )
    }

    if userValidationErr := melodycron.ValidateUserField("ansible-cron heartbeat user", options.HeartbeatUser); nil != userValidationErr {
        return "", userValidationErr
    }

    if utf8Err := refuseInvalidUtf8(ansibleCronHeartbeatName, []string{options.HeartbeatUser, job}); nil != utf8Err {
        return "", utf8Err
    }

    return fmt.Sprintf(
        "- name: %s\n  ansible.builtin.cron:\n    name: %s\n    user: %s\n    job: %s\n",
        yamlScalar(instance.TaskNamePrefix+"heartbeat"),
        yamlScalar(ansibleCronHeartbeatName),
        yamlScalar(options.HeartbeatUser),
        yamlScalar(job),
    ), nil
}

/* ansibleCronJobTokens answers the tokens of the entry's command line under the builtin crontab dialect's rule: a Command override replaces the binary and its arguments, and an entry with neither is refused rather than rendered as an empty job. */
func ansibleCronJobTokens(entry melodycron.Entry) ([]string, error) {
    if 0 < len(entry.Command) {
        if "" == strings.Join(entry.Command, "") {
            return nil, exception.NewError(
                fmt.Sprintf("ansible-cron: entry %q has Command but every token is empty; remove the override or supply a non-empty command", entry.Name),
                exceptioncontract.Context{"entry": entry.Name},
                melodycron.ErrEntryEmptyCommand,
            )
        }

        return entry.Command, nil
    }

    if "" == entry.Binary {
        return nil, exception.NewError(
            fmt.Sprintf("ansible-cron: entry %q has empty binary and no command override; nothing to schedule", entry.Name),
            exceptioncontract.Context{"entry": entry.Name},
            melodycron.ErrEntryEmptyCommand,
        )
    }

    return append([]string{entry.Binary}, entry.Args...), nil
}

/* refuseInvalidUtf8 refuses a value yamlScalar would rewrite: an invalid byte is escaped as \xNN and decoded by a YAML reader as U+00NN, so the user or the job the module writes would differ from the one configured, in silence. */
func refuseInvalidUtf8(entryName string, values []string) error {
    for _, value := range values {
        if false == utf8.ValidString(value) {
            return exception.NewError(
                fmt.Sprintf("ansible-cron: entry %q carries a value that is not valid UTF-8; the playbook would silently rewrite it", entryName),
                exceptioncontract.Context{"entry": entryName, "value": value},
                ErrAnsibleCronInvalidUtf8,
            )
        }
    }

    return nil
}

/* yamlScalar emits value as a double-quoted YAML scalar through Go's %q: the escapes Go writes — \" \\ \n \r \t \a \b \f \v, \xNN for a control byte, \uNNNN and \UNNNNNNNN for a code point the terminal would not show — are all read by a YAML double-quoted scalar with the same meaning, and every printable rune passes through verbatim. */
func yamlScalar(value string) string {
    return fmt.Sprintf("%q", value)
}

/* fieldOrEveryValue mirrors the wildcard defaulting the builtin dialects apply: an empty schedule field means every value. */
func fieldOrEveryValue(field string) string {
    if "" == field {
        return "*"
    }

    return field
}

var (
    _ melodycron.Template           = (*AnsibleCronTemplate)(nil)
    _ melodycron.OwnedTemplate      = (*AnsibleCronTemplate)(nil)
    _ melodycron.UserColumnTemplate = (*AnsibleCronTemplate)(nil)
)
