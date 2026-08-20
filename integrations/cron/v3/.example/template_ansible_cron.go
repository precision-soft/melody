package example

import (
    "fmt"
    "strings"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
)

/* ansibleCronOwnershipMarker is this template's own marker, distinct from the builtin one: --prune empties only files whose first bytes carry the marker of the template generating now, so a dialect of the integrator's own declares a line of its own. */
const ansibleCronOwnershipMarker = "# owned by melody:cron:generate (ansible-cron)"

/* AnsibleCronTemplate renders every entry as one ansible.builtin.cron task, so the same registry that drives the in-process runner can also feed a playbook. The five schedule fields map one to one onto the module's minute/hour/day/month/weekday arguments, which is what keeps this dialect a faithful rendering rather than an expression conversion — and what makes it a genuinely userland dialect: the binding ships no ansible template of its own. TaskNamePrefix is the template's own configuration, injected at construction the way every custom template carries its knobs. */
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
    forbidden := []melodycron.ForbiddenCharacter{
        {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the playbook"},
        {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break"},
    }

    var builder strings.Builder
    builder.WriteString(ansibleCronOwnershipMarker + "\n---\n")

    for _, entry := range entries {
        job := entry.Command
        if 0 == len(job) {
            job = append([]string{entry.Binary}, entry.Args...)
        }

        if validationErr := melodycron.ValidateNoForbiddenCharacters(job, forbidden, "ansible-cron entry "+entry.Name); nil != validationErr {
            return "", validationErr
        }

        schedule := entry.Schedule
        if nil == schedule {
            schedule = &melodycron.Schedule{}
        }

        fmt.Fprintf(
            &builder,
            "- name: %q\n  ansible.builtin.cron:\n    name: %q\n    minute: %q\n    hour: %q\n    day: %q\n    month: %q\n    weekday: %q\n    user: %q\n    job: %q\n",
            instance.TaskNamePrefix+entry.Name,
            entry.Name,
            fieldOrEveryValue(schedule.Minute),
            fieldOrEveryValue(schedule.Hour),
            fieldOrEveryValue(schedule.DayOfMonth),
            fieldOrEveryValue(schedule.Month),
            fieldOrEveryValue(schedule.DayOfWeek),
            entry.User,
            strings.Join(job, " "),
        )
    }

    if "" != options.HeartbeatPath {
        if validationErr := melodycron.ValidateNoForbiddenCharacters([]string{options.HeartbeatPath}, forbidden, "ansible-cron heartbeat path"); nil != validationErr {
            return "", validationErr
        }

        fmt.Fprintf(
            &builder,
            "- name: %q\n  ansible.builtin.cron:\n    name: %q\n    user: %q\n    job: %q\n",
            instance.TaskNamePrefix+"heartbeat",
            "melody heartbeat",
            options.HeartbeatUser,
            "/bin/touch "+options.HeartbeatPath,
        )
    }

    return builder.String(), nil
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
