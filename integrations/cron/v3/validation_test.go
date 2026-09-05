package cron

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestValidateUserFieldRejectsForbiddenCharacter(t *testing.T) {
    if false == errors.Is(ValidateUserField("user", "svc%role"), ErrForbiddenCharacter) {
        t.Fatalf("expected ErrForbiddenCharacter for a user token containing a crontab line-continuation %%")
    }

    if nil != ValidateUserField("user", "deploy") {
        t.Fatalf("expected a normal user token to validate")
    }
}

func TestValidateNoForbiddenCharsRejectsForbiddenChar(t *testing.T) {
    err := ValidateNoForbiddenCharacters([]string{"clean", "with%percent"}, CrontabForbiddenCharacters, "test context")
    if nil == err {
        t.Fatalf("expected error for token containing %%")
    }

    if false == strings.Contains(err.Error(), "test context") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }

    if false == strings.Contains(err.Error(), "with%percent") {
        t.Fatalf("expected error to quote the offending token, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsAllowsCleanTokens(t *testing.T) {
    err := ValidateNoForbiddenCharacters([]string{"safe", "tokens", "only"}, CrontabForbiddenCharacters, "test context")
    if nil != err {
        t.Fatalf("expected nil error for clean tokens, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsWithCustomList(t *testing.T) {
    custom := []ForbiddenCharacter{
        {Char: '\t', Reason: "tabs break YAML"},
    }

    err := ValidateNoForbiddenCharacters([]string{"has\ttab"}, custom, "yaml entry")
    if nil == err {
        t.Fatalf("expected error for tab character")
    }

    if false == strings.Contains(err.Error(), "yaml entry") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsEmptyTokensReturnsNil(t *testing.T) {
    err := ValidateNoForbiddenCharacters(nil, CrontabForbiddenCharacters, "test context")
    if nil != err {
        t.Fatalf("expected nil error for empty tokens, got: %v", err)
    }
}

/* crond treats one out-of-range field as a parse error and refuses the whole crontab file with it, so generation must fail on the same bounds the in-process matcher enforces */
func TestValidateScheduleFieldsRejectsOutOfRangeValues(t *testing.T) {
    cases := []struct {
        field    string
        schedule *Schedule
    }{
        {"Minute", &Schedule{Minute: "60"}},
        {"Hour", &Schedule{Hour: "24"}},
        {"DayOfMonth", &Schedule{DayOfMonth: "32"}},
        {"Month", &Schedule{Month: "13"}},
        {"DayOfWeek", &Schedule{DayOfWeek: "8"}},
        {"Minute range", &Schedule{Minute: "10-60"}},
        {"Minute integer", &Schedule{Minute: "1x"}},
        {"Month name", &Schedule{Month: "foo"}},
        {"Minute name", &Schedule{Minute: "mon"}},
        {"Month glued name", &Schedule{Month: "1jan"}},
        {"DayOfWeek step name", &Schedule{DayOfWeek: "*/mon"}},
    }

    for _, testCase := range cases {
        err := ValidateScheduleFields(Entry{Name: "job", Schedule: testCase.schedule}, CrontabForbiddenCharacters, RunnerDialectCrontab)
        if nil == err {
            t.Fatalf("expected an out-of-range %s to fail generation, got nil", testCase.field)
        }

        if false == errors.Is(err, ErrInvalidSchedule) {
            t.Fatalf("expected errors.Is(err, ErrInvalidSchedule) for %s, got: %v", testCase.field, err)
        }
    }
}

/* day of week 7 is the Sunday alias vixie crond accepts, while the robfig scheduler behind the k8s template bounds the field at 6 */
func TestValidateScheduleFieldsDayOfWeekSevenPerDialect(t *testing.T) {
    entry := Entry{Name: "job", Schedule: &Schedule{DayOfWeek: "7"}}

    if err := ValidateScheduleFields(entry, CrontabForbiddenCharacters, RunnerDialectCrontab); nil != err {
        t.Fatalf("expected day of week 7 to pass under the crontab dialect, got: %v", err)
    }

    if err := ValidateScheduleFields(entry, k8sScheduleForbiddenCharacters, RunnerDialectKubernetes); nil == err {
        t.Fatalf("expected day of week 7 to fail under the kubernetes dialect")
    }
}

/* the robfig scheduler reads a whole-field "?" as the wildcard (the Quartz day-field convention); crond has no "?" and the crontab dialect must keep refusing it */
func TestValidateScheduleFieldsQuestionMarkPerDialect(t *testing.T) {
    for _, schedule := range []*Schedule{
        {DayOfMonth: "?"},
        {DayOfWeek: "?"},
    } {
        entry := Entry{Name: "job", Schedule: schedule}

        if err := ValidateScheduleFields(entry, k8sScheduleForbiddenCharacters, RunnerDialectKubernetes); nil != err {
            t.Fatalf("expected the question mark to pass under the kubernetes dialect, got: %v", err)
        }

        if err := ValidateScheduleFields(entry, CrontabForbiddenCharacters, RunnerDialectCrontab); nil == err {
            t.Fatalf("expected the question mark to fail under the crontab dialect")
        }
    }

    if err := ValidateScheduleFields(Entry{Name: "job", Schedule: &Schedule{Minute: "?"}}, k8sScheduleForbiddenCharacters, RunnerDialectKubernetes); nil == err {
        t.Fatalf("expected the question mark to stay day-field-only under the kubernetes dialect")
    }
}

func TestValidateScheduleFieldsAcceptsValidShapes(t *testing.T) {
    cases := []*Schedule{
        {Minute: "5-59/15"},
        {Minute: "*/15", Hour: "9-17", DayOfMonth: "1,15", Month: "JAN,dec", DayOfWeek: "mon-fri"},
        {DayOfWeek: "sun-sat/2"},
        {Minute: "0", Hour: "3", DayOfMonth: "1", Month: "12", DayOfWeek: "0"},
        {},
    }

    for index, schedule := range cases {
        if err := ValidateScheduleFields(Entry{Name: "job", Schedule: schedule}, CrontabForbiddenCharacters, RunnerDialectCrontab); nil != err {
            t.Fatalf("expected schedule %d to pass, got: %v", index, err)
        }
    }
}

func TestBusyboxDayFieldsDiverge_FlagsThePairsBusyboxRunsDifferently(t *testing.T) {
    divergent := map[string][2]string{
        "full-coverage day of week beside a restricted day of month": {"16", "0-6"},
        "full-coverage via the sunday alias":                         {"16", "0-7"},
        "stepped wildcard day of month beside a restricted weekday":  {"*/2", "1"},
        "full-coverage day of month beside a restricted weekday":     {"1-31", "1"},
        "two stepped wildcards":                                      {"*/2", "*/2"},
    }

    for name, pair := range divergent {
        if false == busyboxDayFieldsDiverge(pair[0], pair[1]) {
            t.Fatalf("%s: expected the pair (%q, %q) to diverge", name, pair[0], pair[1])
        }
    }
}

func TestBusyboxDayFieldsDiverge_LeavesAMalformedFieldToTheScheduleValidation(t *testing.T) {
    if true == busyboxDayFieldsDiverge("not-a-field", "1") {
        t.Fatal("a malformed day of month must not be reported as busybox divergence")
    }

    if true == busyboxDayFieldsDiverge("1", "not-a-field") {
        t.Fatal("a malformed day of week must not be reported as busybox divergence")
    }
}

func TestBusyboxDayFieldsDiverge_PassesThePairsEveryTargetReadsAlike(t *testing.T) {
    agreeing := map[string][2]string{
        "both plain wildcards":                            {"*", "*"},
        "restricted day of month beside a plain wildcard": {"15", "*"},
        "restricted weekday beside a plain wildcard":      {"*", "1"},
        "two restricted partial fields":                   {"15", "1"},
        "stepped wildcard beside a plain wildcard":        {"*/2", "*"},
        "full-coverage weekday beside a plain wildcard":   {"*", "0-6"},
    }

    for name, pair := range agreeing {
        if true == busyboxDayFieldsDiverge(pair[0], pair[1]) {
            t.Fatalf("%s: expected the pair (%q, %q) to agree", name, pair[0], pair[1])
        }
    }
}

func TestValidateUserField_RefusesEveryUnicodeSpace(t *testing.T) {
    spellings := map[string]string{
        "vertical tab": "deploy\vrole",
        "form feed":    "deploy\frole",
        "no-break":     "deploy\u00a0role",
        "ideographic":  "deploy\u3000role",
        "plain space":  "deploy role",
        "tab":          "deploy\trole",
        "newline":      "deploy\nrole",
        "carriage":     "deploy\rrole",
    }

    for spelling, value := range spellings {
        err := ValidateUserField("user", value)
        if nil == err {
            t.Fatalf("%s: a user carrying whitespace must be refused", spelling)
        }

        if false == errors.Is(err, ErrFieldContainsWhitespace) && false == errors.Is(err, ErrForbiddenCharacter) {
            t.Fatalf("%s: expected a whitespace refusal, got %v", spelling, err)
        }
    }
}

/* the wrapping refusal names the entry, the field and the value; the parse error beneath it carries the bounds and the dialect that chose them. A reader of the outer record sees the first, the journal's cause chain walks to the second — so the pin unwraps one link, where the frozen majors' generator validation deliberately names no dialect because theirs judges everything against the crontab limits alone. */
func TestValidateScheduleFields_TheRefusalCauseNamesTheFieldAndTheDialect(t *testing.T) {
    err := ValidateScheduleFields(
        Entry{Name: "job:probe", Schedule: &Schedule{DayOfWeek: "7"}},
        CrontabForbiddenCharacters,
        RunnerDialectKubernetes,
    )
    if nil == err {
        t.Fatal("expected the kubernetes dialect to refuse a day of week of 7")
    }

    var outer *exception.Error
    if false == errors.As(err, &outer) {
        t.Fatalf("expected a melody error, got %T", err)
    }

    var inner *exception.Error
    if false == errors.As(errors.Unwrap(outer), &inner) {
        t.Fatalf("expected the parse refusal beneath the wrapper, got %v", errors.Unwrap(outer))
    }

    if "DayOfWeek" != inner.Context()["field"] {
        t.Fatalf("expected the failing field named in the cause, got %v", inner.Context())
    }

    if string(RunnerDialectKubernetes) != inner.Context()["dialect"] {
        t.Fatalf("expected the dialect that chose the bound named in the cause, got %v", inner.Context())
    }
}
