package example

import (
    "errors"
    "strings"
    "testing"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
)

func ansibleSampleEntry(mutate func(entry *melodycron.Entry)) melodycron.Entry {
    entry := melodycron.Entry{
        Name:          "billing:cleanup",
        User:          "www-data",
        Binary:        "/srv/app/bin/app",
        Args:          []string{"billing:cleanup"},
        Schedule:      &melodycron.Schedule{Minute: "*/15"},
        InstanceIndex: 1,
        InstanceCount: 1,
    }
    if nil != mutate {
        mutate(&entry)
    }

    return entry
}

func ansibleSampleTemplate() *AnsibleCronTemplate {
    return &AnsibleCronTemplate{TaskNamePrefix: "billing cron: "}
}

func renderAnsibleEntry(t *testing.T, mutate func(entry *melodycron.Entry)) string {
    t.Helper()

    content, err := ansibleSampleTemplate().Render([]melodycron.Entry{ansibleSampleEntry(mutate)}, melodycron.RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    return content
}

func renderAnsibleEntryError(t *testing.T, mutate func(entry *melodycron.Entry)) error {
    t.Helper()

    _, err := ansibleSampleTemplate().Render([]melodycron.Entry{ansibleSampleEntry(mutate)}, melodycron.RenderOptions{})
    if nil == err {
        t.Fatalf("expected Render to refuse the entry, got nil")
    }

    return err
}

func assertRefusal(t *testing.T, err error, sentinel error, fragment string) {
    t.Helper()

    if false == errors.Is(err, sentinel) {
        t.Fatalf("expected %v, got: %v", sentinel, err)
    }

    if false == strings.Contains(err.Error(), fragment) {
        t.Fatalf("expected the refusal to name %q, got: %v", fragment, err)
    }
}

func TestAnsibleCronRenderEmitsOneTaskPerEntryUnderTheMarker(t *testing.T) {
    content := renderAnsibleEntry(t, nil)

    expected := ansibleCronOwnershipMarker + "\n---\n" +
        "- name: \"billing cron: billing:cleanup\"\n" +
        "  ansible.builtin.cron:\n" +
        "    name: \"billing:cleanup\"\n" +
        "    minute: \"*/15\"\n" +
        "    hour: \"*\"\n" +
        "    day: \"*\"\n" +
        "    month: \"*\"\n" +
        "    weekday: \"*\"\n" +
        "    user: \"www-data\"\n" +
        "    job: \"/srv/app/bin/app billing:cleanup\"\n"

    if expected != content {
        t.Fatalf("rendered playbook differs from the expected one:\n%s\n--- expected ---\n%s", content, expected)
    }
}

func TestAnsibleCronRenderWithoutEntriesCarriesTheMarkerAlone(t *testing.T) {
    content, err := ansibleSampleTemplate().Render(nil, melodycron.RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if ansibleCronOwnershipMarker+"\n---\n" != content {
        t.Fatalf("expected the marker and the document start alone, got:\n%s", content)
    }
}

func TestAnsibleCronRenderDefaultsANilScheduleToEveryValue(t *testing.T) {
    content := renderAnsibleEntry(t, func(entry *melodycron.Entry) { entry.Schedule = nil })

    for _, field := range []string{"minute", "hour", "day", "month", "weekday"} {
        if false == strings.Contains(content, "    "+field+": \"*\"\n") {
            t.Fatalf("expected %s to default to the wildcard, got:\n%s", field, content)
        }
    }
}

func TestAnsibleCronRenderShellQuotesAnArgumentCarryingASpace(t *testing.T) {
    content := renderAnsibleEntry(t, func(entry *melodycron.Entry) { entry.Args = []string{"billing:cleanup", "a b"} })

    if false == strings.Contains(content, "    job: \"/srv/app/bin/app billing:cleanup 'a b'\"\n") {
        t.Fatalf("expected the argument to be quoted as one word, got:\n%s", content)
    }

    if true == strings.Contains(content, "billing:cleanup a b\"") {
        t.Fatalf("the argument reached the job unquoted, as two words:\n%s", content)
    }
}

func TestAnsibleCronRenderShellQuotesCommandOverrideTokens(t *testing.T) {
    content := renderAnsibleEntry(t, func(entry *melodycron.Entry) { entry.Command = []string{"sh", "-c", "x; curl u | sh"} })

    if false == strings.Contains(content, "    job: \"sh -c 'x; curl u | sh'\"\n") {
        t.Fatalf("expected the override token to be quoted as one word, got:\n%s", content)
    }
}

func TestAnsibleCronRenderRejectsPercentInAnArgument(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Args = []string{"billing:cleanup", "--format=%Y-%m-%d"} })

    assertRefusal(t, err, melodycron.ErrForbiddenCharacter, "--format=%Y-%m-%d")
}

func TestAnsibleCronRenderRejectsPercentInAScheduleField(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Schedule = &melodycron.Schedule{Minute: "0%5"} })

    assertRefusal(t, err, melodycron.ErrForbiddenCharacter, "Schedule.Minute")
}

func TestAnsibleCronRenderRejectsWhitespaceInAScheduleField(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Schedule = &melodycron.Schedule{Minute: "1 * * * * root evil #"} })

    assertRefusal(t, err, melodycron.ErrFieldContainsWhitespace, "Schedule.Minute")
}

func TestAnsibleCronRenderRejectsAScheduleFieldOutOfBounds(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Schedule = &melodycron.Schedule{Minute: "61"} })

    assertRefusal(t, err, melodycron.ErrInvalidSchedule, "Schedule.Minute")
}

func TestAnsibleCronRenderRejectsWhitespaceInTheUser(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.User = "root evil" })

    assertRefusal(t, err, melodycron.ErrFieldContainsWhitespace, "\"root evil\"")
}

func TestAnsibleCronRenderRejectsAnEmptyUser(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.User = "" })

    assertRefusal(t, err, melodycron.ErrEntryEmptyUser, "billing:cleanup")
}

func TestAnsibleCronRenderRejectsAnEntryWithNeitherBinaryNorCommand(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) {
        entry.Binary = ""
        entry.Args = nil
    })

    assertRefusal(t, err, melodycron.ErrEntryEmptyCommand, "empty binary")
}

func TestAnsibleCronRenderRejectsACommandOverrideOfEmptyTokens(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Command = []string{"", ""} })

    assertRefusal(t, err, melodycron.ErrEntryEmptyCommand, "every token is empty")
}

func TestAnsibleCronRenderRejectsALineTerminatorInTheCronName(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Name = "billing:cleanup\n* * * * * root evil" })

    assertRefusal(t, err, melodycron.ErrForbiddenCharacter, "#Ansible:")
}

func TestAnsibleCronRenderRejectsAValueThatIsNotValidUtf8(t *testing.T) {
    err := renderAnsibleEntryError(t, func(entry *melodycron.Entry) { entry.Args = []string{"billing:cleanup", "a\x85b"} })

    assertRefusal(t, err, ErrAnsibleCronInvalidUtf8, "not valid UTF-8")
}

func TestAnsibleCronRenderSuffixesTheCronNameWithTheInstance(t *testing.T) {
    entries := []melodycron.Entry{
        ansibleSampleEntry(func(entry *melodycron.Entry) {
            entry.Args = []string{"billing:cleanup", "--max-instances=2", "--instance-index=1"}
            entry.InstanceIndex = 1
            entry.InstanceCount = 2
        }),
        ansibleSampleEntry(func(entry *melodycron.Entry) {
            entry.Args = []string{"billing:cleanup", "--max-instances=2", "--instance-index=2"}
            entry.InstanceIndex = 2
            entry.InstanceCount = 2
        }),
    }

    content, err := ansibleSampleTemplate().Render(entries, melodycron.RenderOptions{})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    for _, fragment := range []string{
        "- name: \"billing cron: billing:cleanup (1/2)\"\n  ansible.builtin.cron:\n    name: \"billing:cleanup (1/2)\"\n",
        "- name: \"billing cron: billing:cleanup (2/2)\"\n  ansible.builtin.cron:\n    name: \"billing:cleanup (2/2)\"\n",
    } {
        if false == strings.Contains(content, fragment) {
            t.Fatalf("expected the playbook to contain %q, got:\n%s", fragment, content)
        }
    }

    if true == strings.Contains(content, "    name: \"billing:cleanup\"\n") {
        t.Fatalf("an instance rendered under the bare cron name, which the module would treat as the same line:\n%s", content)
    }
}

func TestAnsibleCronRenderRejectsTwoEntriesRenderingOneCronName(t *testing.T) {
    entries := []melodycron.Entry{ansibleSampleEntry(nil), ansibleSampleEntry(nil)}

    _, err := ansibleSampleTemplate().Render(entries, melodycron.RenderOptions{})
    if nil == err {
        t.Fatalf("expected Render to refuse two entries under one cron name, got nil")
    }

    assertRefusal(t, err, ErrAnsibleCronDuplicateName, "\"billing:cleanup\"")
}

func TestAnsibleCronRenderQuotesTheHeartbeatPath(t *testing.T) {
    content, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatUser: "www-data", HeartbeatPath: "/var/lib/a b/hb"},
    )
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expected := "- name: \"billing cron: heartbeat\"\n  ansible.builtin.cron:\n    name: \"melody heartbeat\"\n    user: \"www-data\"\n    job: \"/bin/touch '/var/lib/a b/hb'\"\n"
    if false == strings.HasSuffix(content, expected) {
        t.Fatalf("expected the playbook to end with the quoted heartbeat task, got:\n%s", content)
    }
}

func TestAnsibleCronRenderPrefersTheHeartbeatCommandOverThePath(t *testing.T) {
    content, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatUser: "www-data", HeartbeatPath: "/var/lib/hb", HeartbeatCommand: []string{"/bin/hb", "x y"}},
    )
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "    job: \"/bin/hb 'x y'\"\n") {
        t.Fatalf("expected the heartbeat command to be rendered, quoted, got:\n%s", content)
    }

    if true == strings.Contains(content, "/bin/touch") {
        t.Fatalf("the heartbeat path was rendered beside the heartbeat command:\n%s", content)
    }
}

func TestAnsibleCronRenderRejectsPercentInTheHeartbeatPath(t *testing.T) {
    _, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatUser: "www-data", HeartbeatPath: "/var/lib/%Y/hb"},
    )
    if nil == err {
        t.Fatalf("expected Render to refuse the heartbeat path, got nil")
    }

    assertRefusal(t, err, melodycron.ErrForbiddenCharacter, "heartbeat path")
}

func TestAnsibleCronRenderRejectsAHeartbeatWithoutAUser(t *testing.T) {
    _, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatPath: "/var/lib/hb"},
    )
    if nil == err {
        t.Fatalf("expected Render to refuse the heartbeat without a user, got nil")
    }

    assertRefusal(t, err, melodycron.ErrHeartbeatUserMissing, "heartbeat user")
}

func TestAnsibleCronRenderRejectsWhitespaceInTheHeartbeatUser(t *testing.T) {
    _, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatUser: "www data", HeartbeatPath: "/var/lib/hb"},
    )
    if nil == err {
        t.Fatalf("expected Render to refuse the heartbeat user, got nil")
    }

    assertRefusal(t, err, melodycron.ErrFieldContainsWhitespace, "\"www data\"")
}

func TestAnsibleCronYamlScalarWritesTheEscapesAYamlReaderReads(t *testing.T) {
    if "\"a\\\"b\\\\c\\n\\t\\x00\"" != yamlScalar("a\"b\\c\n\t\x00") {
        t.Fatalf("yamlScalar wrote %s", yamlScalar("a\"b\\c\n\t\x00"))
    }

    if "\"plain: value\"" != yamlScalar("plain: value") {
        t.Fatalf("yamlScalar rewrote a printable value: %s", yamlScalar("plain: value"))
    }
}

func TestAnsibleCronRenderRejectsPercentInTheHeartbeatCommand(t *testing.T) {
    _, err := ansibleSampleTemplate().Render(
        []melodycron.Entry{ansibleSampleEntry(nil)},
        melodycron.RenderOptions{HeartbeatUser: "www-data", HeartbeatCommand: []string{"/bin/date", "+%s"}},
    )
    if nil == err {
        t.Fatalf("expected Render to refuse the heartbeat command, got nil")
    }

    assertRefusal(t, err, melodycron.ErrForbiddenCharacter, "heartbeat command")
}
